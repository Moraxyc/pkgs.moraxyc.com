package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/snapshot"
)

func decisionConfig(t *testing.T, baseURL string) config.Config {
	t.Helper()
	c := config.Config{}
	c.Repository.BaseURL = baseURL
	c.Repository.ValidUntilDays = 7
	return c
}

func decisionSnapshots(t *testing.T) (snapshot.Snapshot, snapshot.Snapshot) {
	t.Helper()
	previous := snapshot.Snapshot{Schema: 1, Projects: []snapshot.Project{{ID: "alpha", Repository: "moraxyc/alpha", Releases: []snapshot.Release{{ID: 1, Tag: "v1", PublishedAt: "2026-11-01T00:00:00Z"}}}}}
	previous.Collection.CompletedAt = "2026-11-14T16:56:07Z"
	b, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	collected := snapshot.Snapshot{}
	if err = json.Unmarshal(b, &collected); err != nil {
		t.Fatal(err)
	}
	collected.Collection.CompletedAt = "2026-11-14T22:17:00Z"
	return previous, collected
}

func TestPublicationRequiredSkipsUnchangedLiveTree(t *testing.T) {
	published := int64(1794675367)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Date: Sat, 14 Nov 2026 16:56:07 +0000\n"))
	}))
	defer server.Close()
	previous, collected := decisionSnapshots(t)
	publish, err := publicationRequired(decisionConfig(t, server.URL+"/deb"), previous, collected, published, published+3600)
	if err != nil {
		t.Fatal(err)
	}
	if publish {
		t.Fatal("unchanged inputs must not republish")
	}
}

func TestPublicationRequiredRepublishes(t *testing.T) {
	published := int64(1794675367)
	previous, collected := decisionSnapshots(t)
	cases := map[string]func(*snapshot.Snapshot, int64) (snapshot.Snapshot, int64){
		"changed inputs": func(s *snapshot.Snapshot, epoch int64) (snapshot.Snapshot, int64) {
			s.Projects[0].Releases = append(s.Projects[0].Releases, snapshot.Release{ID: 2, Tag: "v2", PublishedAt: "2026-11-02T00:00:00Z"})
			return *s, epoch + 3600
		},
		"expiry headroom": func(s *snapshot.Snapshot, epoch int64) (snapshot.Snapshot, int64) {
			return *s, epoch + 6*86400
		},
		"stale live tree": func(s *snapshot.Snapshot, epoch int64) (snapshot.Snapshot, int64) {
			return *s, epoch + 3600
		},
	}
	for name, prepare := range cases {
		next, now := prepare(&collected, published)
		live := "Sat, 14 Nov 2026 16:56:07 +0000"
		if name == "stale live tree" {
			live = "Sat, 14 Nov 2026 10:00:00 +0000"
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("Date: " + live + "\n"))
		}))
		publish, err := publicationRequired(decisionConfig(t, server.URL+"/deb"), previous, next, published, now)
		server.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !publish {
			t.Fatalf("%s: expected a republish", name)
		}
	}
}

func TestPublicationRequiredTreatsMissingLiveTreeAsStale(t *testing.T) {
	published := int64(1794675367)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()
	previous, collected := decisionSnapshots(t)
	publish, err := publicationRequired(decisionConfig(t, server.URL+"/deb"), previous, collected, published, published+3600)
	if err != nil {
		t.Fatal(err)
	}
	if !publish {
		t.Fatal("an unreadable live tree must republish")
	}
}

func TestPublicationRequiredBootstrapsWithoutEpoch(t *testing.T) {
	publish, err := publicationRequired(decisionConfig(t, "http://127.0.0.1:1/deb"), snapshot.Snapshot{}, snapshot.Snapshot{}, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !publish {
		t.Fatal("a missing epoch must publish")
	}
}
