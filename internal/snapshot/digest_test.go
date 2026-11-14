package snapshot

import (
	"testing"
)

func digestSnapshot(t *testing.T, s Snapshot) [32]byte {
	t.Helper()
	d, err := s.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDigestIgnoresVolatileTimestamps(t *testing.T) {
	base := Snapshot{Schema: 1, Projects: []Project{{ID: "alpha", Repository: "moraxyc/alpha", Releases: []Release{}}}}
	base.Collection.CompletedAt = "2026-11-14T16:56:07Z"
	base.IndexHistory = map[string]HistoricalIndex{
		"deb/dists/stable/main/binary-amd64/by-hash/SHA256/aa": {ExpiresAt: 1795280467, Data: []byte("index")},
	}
	moved := base
	moved.Collection.CompletedAt = "2026-11-15T03:17:00Z"
	moved.IndexHistory = map[string]HistoricalIndex{
		"deb/dists/stable/main/binary-amd64/by-hash/SHA256/aa": {ExpiresAt: 1795300000, Data: []byte("index")},
	}
	if digestSnapshot(t, base) != digestSnapshot(t, moved) {
		t.Fatal("volatile timestamps changed the digest")
	}
}

func TestDigestTracksPublicationInputs(t *testing.T) {
	base := Snapshot{Schema: 1, Projects: []Project{{ID: "alpha", Repository: "moraxyc/alpha", Releases: []Release{}}}}
	base.Collection.CompletedAt = "2026-11-14T16:56:07Z"
	cases := map[string]func(*Snapshot){
		"schema":  func(s *Snapshot) { s.Schema = 2 },
		"project": func(s *Snapshot) { s.Projects[0].Repository = "moraxyc/beta" },
		"release": func(s *Snapshot) {
			s.Projects[0].Releases = append(s.Projects[0].Releases, Release{ID: 1, Tag: "v1"})
		},
		"history": func(s *Snapshot) {
			s.IndexHistory = map[string]HistoricalIndex{"deb/dists/stable/main/binary-amd64/by-hash/SHA256/bb": {ExpiresAt: 1, Data: []byte("other")}}
		},
	}
	baseDigest := digestSnapshot(t, base)
	for name, mutate := range cases {
		changed := base
		changed.Projects = append([]Project(nil), base.Projects...)
		changed.IndexHistory = map[string]HistoricalIndex{}
		for key, entry := range base.IndexHistory {
			changed.IndexHistory[key] = entry
		}
		mutate(&changed)
		if digestSnapshot(t, changed) == baseDigest {
			t.Fatalf("%s did not change the digest", name)
		}
	}
}
