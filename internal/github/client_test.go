package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/projects"
	"pkgs.moraxyc.com/internal/snapshot"
	"strings"
	"testing"
	"time"
)

func TestFullCollectionRejectsIncompleteResponses(t *testing.T) {
	c, e := config.LoadRepositoryConfig("../../tests/fixtures/config/repository.toml")
	if e != nil {
		t.Fatal(e)
	}
	ps, e := projects.DiscoverProjects("../../tests/fixtures/projects", c)
	if e != nil {
		t.Fatal(e)
	}
	s, e := snapshot.Load("../../tests/fixtures/snapshot.json")
	if e != nil {
		t.Fatal(e)
	}
	mode := ""
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for _, p := range s.Projects {
			prefix := "/repos/" + p.Repository + "/releases"
			if r.URL.Path == prefix {
				if mode == "rate-limit" {
					w.Header().Set("Retry-After", "60")
					w.WriteHeader(429)
					return
				}
				if r.URL.Query().Get("page") == "2" {
					json.NewEncoder(w).Encode([]any{map[string]any{"id": 999, "draft": true, "prerelease": false}, map[string]any{"id": 998, "draft": false, "prerelease": true}})
					return
				}
				w.Header().Set("Link", "<"+server.URL+prefix+"?page=2>; rel=\"next\"")
				if mode == "cross-origin" {
					w.Header().Set("Link", "<https://example.invalid/stolen?page=2>; rel=\"next\"")
				}
				if mode == "cycle" {
					w.Header().Set("Link", "<"+server.URL+prefix+"?page=1>; rel=\"next\"")
				}
				rows := []any{}
				for _, release := range p.Releases {
					rows = append(rows, map[string]any{"id": release.ID, "tag_name": release.Tag, "draft": false, "prerelease": release.Prerelease, "published_at": release.PublishedAt})
				}
				json.NewEncoder(w).Encode(rows)
				return
			}
			for _, release := range p.Releases {
				manifestBytes, _ := release.Manifest.Bytes()
				assets := append(release.Assets[:0:0], release.Assets...)
				for i := range assets {
					if assets[i].Name == "moraxyc-manifest.json" {
						assets[i].Size = int64(len(manifestBytes))
					}
				}
				id := func(n int64) string { b, _ := json.Marshal(n); return string(b) }
				if r.URL.Path == prefix+"/"+id(release.ID)+"/assets" {
					if mode == "missing-manifest" {
						assets = assets[:len(assets)-1]
					}
					if mode == "duplicate-asset" {
						assets = append(assets, assets[0])
					}
					json.NewEncoder(w).Encode(assets)
					return
				}
				for _, asset := range assets {
					if r.URL.Path == prefix+"/assets/"+id(asset.ID) {
						if strings.HasSuffix(asset.Name, ".deb") {
							t.Error("collector downloaded .deb")
							w.WriteHeader(500)
							return
						}
						if mode == "corrupt-json" {
							w.Write([]byte("{"))
							return
						}
						if mode == "oversize" {
							w.Write([]byte(strings.Repeat(" ", 1<<20+1)))
							return
						}
						w.Write(manifestBytes)
						return
					}
				}
			}
		}
		t.Errorf("unexpected API request %s", r.URL.Path)
		w.WriteHeader(404)
	}))
	defer server.Close()
	client := New("")
	client.BaseURL = server.URL
	client.HTTP = server.Client()
	now := time.Date(2026, 9, 15, 2, 1, 0, 0, time.UTC)
	got, e := client.Collect(context.Background(), c, ps, now)
	if e != nil {
		t.Fatal(e)
	}
	records, e := got.Records(c, ps)
	if e != nil || len(records) != 5 {
		t.Fatalf("incomplete collection: %v", e)
	}
	mode = "cross-origin"
	if _, err := client.Collect(context.Background(), c, ps, now); err != nil {
		t.Fatalf("pagination should use only the page number on the original resource: %v", err)
	}
	for _, failure := range []string{"missing-manifest", "duplicate-asset", "corrupt-json", "oversize", "cycle", "rate-limit"} {
		t.Run(failure, func(t *testing.T) {
			mode = failure
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, e := client.Collect(ctx, c, ps, now)
			if e == nil {
				t.Fatal("failed refresh succeeded")
			}
		})
	}
}

func TestAssetDownloadDoesNotForwardAPICredentials(t *testing.T) {
	assetData := []byte("release asset")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedAuth := ""
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			expectedAuth = "Bearer synthetic-token"
		}
		if r.Header.Get("Authorization") != expectedAuth {
			t.Error("asset request crossed its authentication boundary")
		}
		switch r.URL.Path {
		case "/repos/acme/demo/releases/assets/8":
			http.Redirect(w, r, "https://release-assets.githubusercontent.com/release-assets/8", http.StatusFound)
		case "/release-assets/8":
			http.Redirect(w, r, "https://objects.githubusercontent.com/release-assets/final", http.StatusFound)
		case "/release-assets/final":
			w.Write(assetData)
		default:
			t.Errorf("unexpected asset request: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := New("synthetic-token")
	client.BaseURL = server.URL
	client.HTTP = server.Client()
	transport := client.HTTP.Transport
	endpoint, _ := url.Parse(server.URL)
	client.HTTP.Transport = testTransport(func(req *http.Request) (*http.Response, error) {
		req = req.Clone(req.Context())
		if req.URL.Host == "release-assets.githubusercontent.com" || req.URL.Host == "objects.githubusercontent.com" {
			req.URL.Scheme, req.URL.Host = endpoint.Scheme, endpoint.Host
		}
		return transport.RoundTrip(req)
	})
	session, err := client.API()
	if err != nil {
		t.Fatal(err)
	}
	downloaded := filepath.Join(t.TempDir(), "downloaded.deb")
	if err := session.DownloadReleaseAsset(context.Background(), "acme", "demo", 8, downloaded, int64(len(assetData))); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(downloaded)
	if err != nil || string(got) != string(assetData) {
		t.Fatalf("downloaded asset = %q, err = %v", got, err)
	}
}

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
