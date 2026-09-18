package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/manifest"
	"pkgs.moraxyc.com/internal/projects"
	"pkgs.moraxyc.com/internal/safe"
	"sort"
	"time"
)

type Release struct {
	ID          int64             `json:"id"`
	Tag         string            `json:"tag"`
	Prerelease  bool              `json:"prerelease"`
	PublishedAt string            `json:"published_at"`
	Assets      []manifest.Asset  `json:"assets"`
	Manifest    manifest.Manifest `json:"manifest"`
}
type Project struct {
	ID         string    `json:"id"`
	Repository string    `json:"repository"`
	Releases   []Release `json:"releases"`
}
type HistoricalIndex struct {
	ExpiresAt int64  `json:"expires_at"`
	Data      []byte `json:"data"`
}
type Snapshot struct {
	IndexHistory map[string]HistoricalIndex `json:"index_history,omitempty"`
	Schema       int                        `json:"schema"`
	Projects     []Project                  `json:"projects"`
	Collection   struct {
		CompletedAt string `json:"completed_at"`
	} `json:"collection"`
}
type Record struct {
	Package    manifest.Package
	Pool       string
	Repository string
	Tag        string
}

func Load(name string) (Snapshot, error) {
	var s Snapshot
	b, e := safe.Regular(name)
	if e != nil {
		return s, e
	}
	if e = safe.JSON(b, &s); e != nil {
		return s, e
	}
	var raw struct {
		Projects []struct {
			Releases []struct {
				Manifest json.RawMessage `json:"manifest"`
			} `json:"releases"`
		} `json:"projects"`
	}
	if e = json.Unmarshal(b, &raw); e != nil {
		return s, errors.New("invalid snapshot JSON")
	}
	for i, p := range raw.Projects {
		for j, r := range p.Releases {
			m, e := manifest.Parse(r.Manifest)
			if e != nil {
				return s, e
			}
			s.Projects[i].Releases[j].Manifest = m
		}
	}
	return s, nil
}
func (s *Snapshot) Normalize() {
	sort.Slice(s.Projects, func(i, j int) bool { return s.Projects[i].ID < s.Projects[j].ID })
	for i := range s.Projects {
		p := &s.Projects[i]
		sort.Slice(p.Releases, func(i, j int) bool { return p.Releases[i].ID < p.Releases[j].ID })
		for j := range p.Releases {
			r := &p.Releases[j]
			sort.Slice(r.Assets, func(i, j int) bool { return r.Assets[i].ID < r.Assets[j].ID })
			r.Manifest.Normalize()
		}
	}
}
func (s Snapshot) Save(name string) error {
	s.Normalize()
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	return safe.Atomic(name, append(b, '\n'))
}
func (s Snapshot) Records(c config.Config, ps []projects.Project) ([]Record, error) {
	if e := c.Validate(); e != nil {
		return nil, e
	}
	if s.Schema != 1 || s.Projects == nil {
		return nil, errors.New("invalid snapshot schema")
	}
	if _, e := time.Parse(time.RFC3339, s.Collection.CompletedAt); e != nil {
		return nil, errors.New("invalid collection timestamp")
	}
	expected := map[string]projects.Project{}
	for _, p := range ps {
		if e := p.Validate(c); e != nil {
			return nil, e
		}
		if p.Source.Enabled {
			if _, ok := expected[p.ID]; ok {
				return nil, errors.New("duplicate project policy")
			}
			expected[p.ID] = p
		}
	}
	seen := map[string]bool{}
	tuples, pools := map[string]bool{}, map[string]bool{}
	out := []Record{}
	for _, sp := range s.Projects {
		p, ok := expected[sp.ID]
		if !ok || seen[sp.ID] || sp.Repository != p.Source.Repository || sp.Releases == nil {
			return nil, errors.New("snapshot project set differs from enabled registry")
		}
		seen[sp.ID] = true
		ids := map[int64]bool{}
		tags := map[string]bool{}
		for _, r := range sp.Releases {
			if r.ID <= 0 || ids[r.ID] || tags[r.Tag] || !safe.Segment(r.Tag) || r.Prerelease && !p.Source.IncludePrereleases {
				return nil, errors.New("invalid or duplicate release")
			}
			ids[r.ID] = true
			tags[r.Tag] = true
			if _, e := time.Parse(time.RFC3339, r.PublishedAt); e != nil {
				return nil, errors.New("invalid release timestamp")
			}
			b, e := json.Marshal(r.Manifest)
			if e != nil {
				return nil, e
			}
			m, e := manifest.Parse(b)
			if e != nil {
				return nil, e
			}
			if e = m.ValidateRelease(c, p, r.Tag, r.Assets); e != nil {
				return nil, fmt.Errorf("project %s tag %s: %w", p.ID, r.Tag, e)
			}
			for _, pkg := range m.Packages {
				pool, e := p.MatchRule(pkg.Package, pkg.Architecture, pkg.Filename, r.Tag)
				if e != nil {
					return nil, e
				}
				if tuples[pkg.Key()] || pools[pool] {
					return nil, errors.New("global package or pool collision")
				}
				tuples[pkg.Key()] = true
				pools[pool] = true
				out = append(out, Record{pkg, pool, p.Source.Repository, r.Tag})
			}
		}
	}
	if len(seen) != len(expected) {
		return nil, errors.New("snapshot is missing an enabled project")
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Package.Key()+"\x00"+out[i].Pool < out[j].Package.Key()+"\x00"+out[j].Pool
	})
	return out, nil
}
