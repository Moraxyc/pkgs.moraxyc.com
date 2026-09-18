package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"pkgs.moraxyc.com/internal/apt"
	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/redirects"
	"pkgs.moraxyc.com/internal/snapshot"
	"sort"
	"strings"
	"time"
)

// Remote checks metadata bytes and redirect headers without following pool URLs.
func Remote(ctx context.Context, baseURL, root string, c config.Config, records []snapshot.Record, history map[string]snapshot.HistoricalIndex, epoch int64) error {
	return remote(ctx, baseURL, root, c, records, history, epoch, 5*time.Second)
}

func remote(ctx context.Context, baseURL, root string, c config.Config, records []snapshot.Record, history map[string]snapshot.HistoricalIndex, epoch int64, interval time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, 7*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	paths := []string{"deb/dists/stable/Release", "deb/dists/stable/Release.gpg", "deb/dists/stable/InRelease", "deb/keys/moraxyc-archive-keyring.gpg"}
	indexes, e := apt.PublicationIndexes(c, records, history, epoch)
	if e != nil {
		return e
	}
	for name := range indexes {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	expectedFiles := map[string][]byte{}
	for _, name := range paths {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return err
		}
		expectedFiles[name] = data
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	get := func(path string) (*http.Response, error) {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/"+path, nil)
		if e != nil {
			return nil, errors.New("invalid smoke URL")
		}
		resp, e := client.Do(req)
		if e != nil {
			return nil, errors.New("smoke request failed")
		}
		return resp, nil
	}
	check := func() error {
		for _, name := range paths {
			expected := expectedFiles[name]
			resp, e := get(name)
			if e != nil {
				return e
			}
			b, e := io.ReadAll(io.LimitReader(resp.Body, int64(len(expected))+1))
			resp.Body.Close()
			if e != nil || resp.StatusCode != 200 || !bytes.Equal(b, expected) {
				return errors.New("deployed metadata mismatch: " + name)
			}
		}
		if len(records) > 0 {
			r := records[0]
			resp, e := get("deb/" + r.Pool)
			if e != nil {
				return e
			}
			resp.Body.Close()
			if resp.StatusCode != 302 || resp.Header.Get("Location") != redirects.Target(r) {
				return errors.New("deployed pool redirect mismatch")
			}
		}
		resp, e := get("deb/pool/moraxyc-nonexistent-smoke/never-published.deb")
		if e != nil {
			return e
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			return errors.New("unknown pool path must return 404")
		}
		return nil
	}
	for {
		err := check()
		if err == nil {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("remote verification did not converge: %v: %w", err, ctx.Err())
		case <-timer.C:
		}
	}
}
