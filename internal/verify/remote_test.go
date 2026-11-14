package verify

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pkgs.moraxyc.com/internal/apt"
	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/safe"
)

func TestRemoteWaitsForConvergenceAndHonorsDeadline(t *testing.T) {
	c, err := config.LoadRepositoryConfig("../../tests/fixtures/config/repository.toml")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	files, err := apt.Indexes(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Release", "Release.gpg", "InRelease"} {
		files["deb/dists/stable/"+name] = []byte("current " + name)
	}
	files["deb/keys/moraxyc-archive-keyring.gpg"] = []byte("synthetic public bytes")
	for name, data := range files {
		if err = safe.Atomic(filepath.Join(root, name), data); err != nil {
			t.Fatal(err)
		}
	}
	var attempts atomic.Int32
	var permanent atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/InRelease") {
			n := attempts.Add(1)
			if permanent.Load() || n == 1 {
				w.Write([]byte("previous deployment"))
				return
			}
			if n == 2 {
				w.WriteHeader(503)
				return
			}
		}
		if data, ok := files[strings.TrimPrefix(r.URL.Path, "/")]; ok {
			w.Write(data)
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var progress bytes.Buffer
	if err := remote(ctx, server.URL, root, c, nil, nil, 1, time.Millisecond, &progress); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() < 3 {
		t.Fatal("accepted stale or unavailable metadata")
	}
	for _, want := range []string{"attempt=1 duration=", "attempt=2 duration=", "attempt=3 duration=", "elapsed=", "deb/dists/stable/InRelease (status=200)", "deb/dists/stable/InRelease (status=503)", "error=<nil>"} {
		if !strings.Contains(progress.String(), want) {
			t.Fatalf("missing diagnostic %q in %s", want, progress.String())
		}
	}
	progress.Reset()
	permanent.Store(true)
	ctx, cancel = context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := remote(ctx, server.URL, root, c, nil, nil, 1, time.Millisecond, &progress); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("persistent mismatch: %v", err)
	}
}
