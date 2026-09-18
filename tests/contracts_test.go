package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/manifest"
	"pkgs.moraxyc.com/internal/projects"
	"pkgs.moraxyc.com/internal/safe"
	"pkgs.moraxyc.com/internal/snapshot"
	"strings"
	"testing"
)

func fixture(t *testing.T) (config.Config, []projects.Project, snapshot.Snapshot) {
	t.Helper()
	c, e := config.LoadRepositoryConfig("fixtures/config/repository.toml")
	must(t, e)
	ps, e := projects.DiscoverProjects("fixtures/projects", c)
	must(t, e)
	s, e := snapshot.Load("fixtures/snapshot.json")
	must(t, e)
	return c, ps, s
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func TestSnapshotRejectsUnpublishableRecords(t *testing.T) {
	c, ps, s := fixture(t)
	rs, e := s.Records(c, ps)
	must(t, e)
	if len(rs) != 5 || len(ps) != 3 || ps[2].Source.Enabled {
		t.Fatal("enabled project records changed")
	}
	cases := map[string]func(*snapshot.Snapshot){
		"missing asset": func(s *snapshot.Snapshot) { s.Projects[0].Releases[0].Assets = s.Projects[0].Releases[0].Assets[1:] },
		"size mismatch": func(s *snapshot.Snapshot) { s.Projects[0].Releases[0].Assets[0].Size++ },
		"digest mismatch": func(s *snapshot.Snapshot) {
			s.Projects[0].Releases[0].Assets[0].Digest = "sha256:" + strings.Repeat("f", 64)
		},
		"duplicate asset":        func(s *snapshot.Snapshot) { r := &s.Projects[0].Releases[0]; r.Assets = append(r.Assets, r.Assets[0]) },
		"wrong project":          func(s *snapshot.Snapshot) { s.Projects[0].Releases[0].Manifest.Project = "beta" },
		"wrong tag":              func(s *snapshot.Snapshot) { s.Projects[0].Releases[0].Manifest.ReleaseTag = "v2" },
		"missing project":        func(s *snapshot.Snapshot) { s.Projects = s.Projects[:1] },
		"disabled project":       func(s *snapshot.Snapshot) { s.Projects[0].ID = "disabled" },
		"duplicate global tuple": func(s *snapshot.Snapshot) { s.Projects[0].Releases[1].Manifest.Packages[0].Version = "1.0-1" },
		"traversal":              func(s *snapshot.Snapshot) { s.Projects[0].Releases[0].Tag = "../v1" },
		"control injection": func(s *snapshot.Snapshot) {
			s.Projects[0].Releases[0].Manifest.Packages[0].Description = "summary\nFilename: evil"
		},
		"unsupported architecture": func(s *snapshot.Snapshot) { s.Projects[0].Releases[0].Manifest.Packages[0].Architecture = "i386" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, s := fixture(t)
			change(&s)
			if _, e := s.Records(c, ps); e == nil {
				t.Fatal("accepted invalid snapshot")
			}
		})
	}
}
func TestSchemaAndMappingRejectAmbiguousInput(t *testing.T) {
	c, ps, s := fixture(t)
	m := s.Projects[0].Releases[0].Manifest
	b, e := m.Bytes()
	must(t, e)
	_, e = manifest.Parse(b)
	must(t, e)
	for _, b := range [][]byte{bytes.Replace(b, []byte(`"schema": 1`), []byte(`"schema": 1, "extra": true`), 1), bytes.Replace(b, []byte(`"schema": 1`), []byte(`"schema": 1, "schema": 1`), 1), bytes.Replace(b, []byte(`"Size": 1234`), []byte(`"Size": "1234"`), 1), bytes.Replace(b, []byte(`"SHA256": "`), []byte(`"SHA256": "A`), 1)} {
		if _, e = manifest.Parse(b); e == nil {
			t.Fatal("accepted malformed manifest")
		}
	}
	p := ps[0]
	pkg := m.Packages[0]
	pool, e := p.MatchRule(pkg.Package, pkg.Architecture, pkg.Filename, m.ReleaseTag)
	must(t, e)
	if pool != "pool/alpha/v1.0-1/alpha_1.0-1_amd64.deb" {
		t.Fatal(pool)
	}
	p.Rules = append(p.Rules, p.Rules[0])
	if _, e = p.MatchRule(pkg.Package, pkg.Architecture, pkg.Filename, m.ReleaseTag); e == nil {
		t.Fatal("ambiguous mapping accepted")
	}
	p.Rules = nil
	if _, e = p.MatchRule(pkg.Package, pkg.Architecture, pkg.Filename, m.ReleaseTag); e == nil {
		t.Fatal("unmapped package accepted")
	}
	for _, path := range []string{"../pool/x.deb", "/pool/x.deb", "pool/{unknown}/x.deb", "https://evil/x.deb", "pool/a//x.deb", "pool/a/../x.deb", "pool/a\\x.deb"} {
		p = ps[0]
		p.Rules = append([]projects.Rule(nil), p.Rules...)
		p.Rules[0].Pool = path
		if p.Validate(c) == nil {
			t.Fatalf("accepted unsafe template %q", path)
		}
	}
	p = ps[0]
	p.Source.Repository = "elsewhere/alpha"
	if p.Validate(c) == nil {
		t.Fatal("cross-organization source accepted")
	}
}
func TestStrictConfigurationAndStableReplay(t *testing.T) {
	c, ps, s := fixture(t)
	b, e := os.ReadFile("fixtures/config/repository.toml")
	must(t, e)
	for _, bad := range []string{strings.Replace(string(b), "valid_until_days = 7", "valid_until_days = 7\nunknown = true", 1), strings.Replace(string(b), `["amd64", "arm64"]`, `["amd64", "amd64"]`, 1), strings.Replace(string(b), "valid_until_days = 7", "", 1), strings.Replace(string(b), "valid_until_days = 7", "valid_until_days = 7\nvalid_until_days = 8", 1), strings.Replace(string(b), `base_url = "https://pkgs.moraxyc.com/deb"`, "base_url = \"https://pkgs.moraxyc.com/deb\"\nBase_URL = \"https://pkgs.moraxyc.com/deb\"", 1)} {
		file := filepath.Join(t.TempDir(), "repository.toml")
		must(t, os.WriteFile(file, []byte(bad), 0600))
		if _, e = config.LoadRepositoryConfig(file); e == nil {
			t.Fatal("accepted invalid config")
		}
	}
	a := filepath.Join(t.TempDir(), "a.json")
	bfile := filepath.Join(t.TempDir(), "b.json")
	must(t, s.Save(a))
	s.Projects[0], s.Projects[1] = s.Projects[1], s.Projects[0]
	for i := range s.Projects {
		rs := s.Projects[i].Releases
		for j, k := 0, len(rs)-1; j < k; j, k = j+1, k-1 {
			rs[j], rs[k] = rs[k], rs[j]
		}
	}
	must(t, s.Save(bfile))
	left, e := os.ReadFile(a)
	must(t, e)
	right, e := os.ReadFile(bfile)
	must(t, e)
	if !bytes.Equal(left, right) {
		t.Fatal("snapshot depends on API ordering")
	}
	_, e = s.Records(c, ps)
	must(t, e)
	badSnapshot := filepath.Join(t.TempDir(), "bad.json")
	var malformed map[string]any
	must(t, json.Unmarshal(left, &malformed))
	firstProject := malformed["projects"].([]any)[0].(map[string]any)
	firstRelease := firstProject["releases"].([]any)[0].(map[string]any)
	firstPackage := firstRelease["manifest"].(map[string]any)["packages"].([]any)[0].(map[string]any)
	firstPackage["Depends"] = nil
	invalid, e := json.Marshal(malformed)
	must(t, e)
	must(t, os.WriteFile(badSnapshot, invalid, 0600))
	if _, e = snapshot.Load(badSnapshot); e == nil {
		t.Fatal("snapshot bypassed manifest schema")
	}
	var raw map[string]any
	must(t, json.Unmarshal(left, &raw))
	if safe.JSON(append(left, []byte(`{}`)...), &raw) == nil {
		t.Fatal("accepted trailing JSON")
	}
}
