package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"pkgs.moraxyc.com/internal/safe"
)

func TestPublishResumesDraftAndRejectsChangedAssets(t *testing.T) {
	base := t.TempDir()
	control := filepath.Join(base, "package", "DEBIAN")
	dist := filepath.Join(base, "dist")
	for _, dir := range []string{control, dist} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(control, "control"), []byte("Package: sample\nVersion: 1.0\nArchitecture: all\nMaintainer: Test <test@example.invalid>\nDescription: Test package\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := safe.Command(nil, "dpkg-deb", "--build", "--root-owner-group", filepath.Dir(control), filepath.Join(dist, "sample.deb")); err != nil {
		t.Fatal(err)
	}
	opts := projectCIOptions{directory: dist, project: "sample", tag: "v1", repository: "acme/sample", commit: strings.Repeat("a", 40), architectures: "amd64"}
	if err := executeProjectCI("manifest", opts); err != nil {
		t.Fatal(err)
	}

	type asset struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Size  int    `json:"size"`
		State string `json:"state"`
		Data  []byte `json:"-"`
	}
	assets := map[int64]*asset{}
	created, published, failUpload := false, false, true
	uploads := map[string]int{}
	readbacks := map[string]bool{}
	var nextID int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		release := func() map[string]any {
			return map[string]any{"id": 7, "tag_name": "v1", "draft": !published, "prerelease": false}
		}
		prefix := "/repos/acme/sample"
		switch {
		case r.URL.Path == prefix+"/commits/v1":
			json.NewEncoder(w).Encode(map[string]string{"sha": opts.commit})
		case r.URL.Path == prefix+"/releases/tags/v1":
			if !published {
				w.WriteHeader(404)
				fmt.Fprint(w, `{}`)
				return
			}
			json.NewEncoder(w).Encode(release())
		case r.URL.Path == prefix+"/releases" && r.Method == "GET":
			rows := []any{}
			if created {
				rows = append(rows, release())
			}
			json.NewEncoder(w).Encode(rows)
		case r.URL.Path == prefix+"/releases" && r.Method == "POST":
			var input struct{ Draft bool }
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || !input.Draft {
				t.Error("incomplete release exposed publicly")
			}
			if created {
				t.Error("retry created a duplicate release")
			}
			created = true
			json.NewEncoder(w).Encode(release())
		case r.URL.Path == prefix+"/releases/7/assets" && r.Method == "POST":
			name := r.URL.Query().Get("name")
			uploads[name]++
			data, _ := io.ReadAll(r.Body)
			nextID++
			a := &asset{ID: nextID, Name: name, Size: len(data), State: "uploaded", Data: data}
			assets[nextID] = a
			if name == "sample.deb" && failUpload {
				failUpload = false
				a.State, a.Size, a.Data = "starter", 0, nil
				w.WriteHeader(502)
				fmt.Fprint(w, `{}`)
				return
			}
			json.NewEncoder(w).Encode(a)
		case r.URL.Path == prefix+"/releases/7/assets":
			rows := []*asset{}
			for _, a := range assets {
				rows = append(rows, a)
			}
			json.NewEncoder(w).Encode(rows)
		case strings.HasPrefix(r.URL.Path, prefix+"/releases/assets/"):
			id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, prefix+"/releases/assets/"), 10, 64)
			a := assets[id]
			if a == nil {
				w.WriteHeader(404)
				return
			}
			if r.Method == "DELETE" {
				if a.State != "starter" || published {
					t.Error("deleted a completed asset")
				}
				delete(assets, id)
				w.WriteHeader(204)
				return
			}
			readbacks[a.Name] = true
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(a.Data)
		case r.URL.Path == prefix+"/releases/7" && r.Method == "PATCH":
			if len(readbacks) != 2 {
				t.Error("published before verifying downloaded assets")
			}
			published = true
			json.NewEncoder(w).Encode(release())
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GITHUB_UPLOAD_URL", server.URL)
	t.Setenv("GH_TOKEN", "synthetic-token")
	if err := executeProjectCI("publish", opts); err == nil {
		t.Fatal("failed upload succeeded")
	}
	if published {
		t.Fatal("failed upload published a release")
	}
	// A completed asset with unexpected bytes must never be overwritten or published.
	var manifestAsset *asset
	for _, a := range assets {
		if a.Name == "moraxyc-manifest.json" {
			manifestAsset = a
		}
	}
	if manifestAsset == nil {
		t.Fatal("missing completed manifest upload")
	}
	original := append([]byte(nil), manifestAsset.Data...)
	manifestAsset.Data[0] = '!'
	if err := executeProjectCI("publish", opts); err == nil || published {
		t.Fatal("changed remote bytes were published")
	}
	manifestAsset.Data = original
	if err := executeProjectCI("publish", opts); err != nil {
		t.Fatal(err)
	}
	if !published {
		t.Fatal("complete release was not published")
	}
	for _, operation := range []string{"publish", "verify"} {
		if err := executeProjectCI(operation, opts); err != nil {
			t.Fatal(err)
		}
	}
	if uploads["moraxyc-manifest.json"] != 1 || uploads["sample.deb"] != 2 {
		t.Fatalf("unexpected repeat uploads: %v", uploads)
	}
}
