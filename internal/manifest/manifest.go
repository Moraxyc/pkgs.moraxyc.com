package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/projects"
	"pkgs.moraxyc.com/internal/safe"
	"pkgs.moraxyc.com/schemas"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type Package struct {
	Package      string
	Version      string
	Architecture string
	Filename     string
	Size         int64
	SHA256       string
	Depends      string `json:",omitempty"`
	Description  string
}
type Manifest struct {
	Schema     int       `json:"schema"`
	Project    string    `json:"project"`
	ReleaseTag string    `json:"release_tag"`
	Packages   []Package `json:"packages"`
}
type Asset struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest,omitempty"`
}

var schemaOnce sync.Once
var schema *jsonschema.Schema
var schemaErr error

func Parse(b []byte) (Manifest, error) {
	var m Manifest
	var v any
	if e := safe.JSON(b, &v); e != nil {
		return m, e
	}
	schemaOnce.Do(func() {
		var doc any
		doc, schemaErr = jsonschema.UnmarshalJSON(bytes.NewReader(schemas.Manifest))
		if schemaErr != nil {
			return
		}
		c := jsonschema.NewCompiler()
		schemaErr = c.AddResource("manifest.json", doc)
		if schemaErr == nil {
			schema, schemaErr = c.Compile("manifest.json")
		}
	})
	if schemaErr != nil {
		return m, errors.New("invalid embedded schema")
	}
	if e := schema.Validate(v); e != nil {
		return m, errors.New("manifest schema validation failed")
	}
	if e := safe.JSON(b, &m); e != nil {
		return m, e
	}
	return m, m.Validate()
}

var version = regexp.MustCompile(`^(?:[0-9]+:)?[0-9][A-Za-z0-9.+:~\-]*$`)

func (p Package) Key() string { return p.Package + "\x00" + p.Version + "\x00" + p.Architecture }
func (m *Manifest) Normalize() {
	sort.Slice(m.Packages, func(i, j int) bool {
		a, b := m.Packages[i], m.Packages[j]
		return a.Key()+"\x00"+a.Filename < b.Key()+"\x00"+b.Filename
	})
}
func (m Manifest) Bytes() ([]byte, error) {
	m.Normalize()
	b, e := json.MarshalIndent(m, "", "  ")
	return append(b, '\n'), e
}
func (m Manifest) Validate() error {
	if m.Schema != 1 || !safe.Identifier(m.Project) || !safe.Segment(m.ReleaseTag) || len(m.Packages) == 0 {
		return errors.New("invalid manifest identity")
	}
	tuples, files := map[string]bool{}, map[string]bool{}
	for i, p := range m.Packages {
		fail := func(field string) error { return fmt.Errorf("package record %d: invalid %s", i, field) }
		if !safe.Package(p.Package) {
			return fail("Package")
		}
		if !version.MatchString(p.Version) {
			return fail("Version")
		}
		upstream := p.Version
		if n := strings.IndexByte(upstream, ':'); n >= 0 {
			for _, digit := range upstream[:n] {
				if digit < '0' || digit > '9' {
					return fail("Version epoch")
				}
			}
			upstream = upstream[n+1:]
		}
		if upstream == "" || upstream[0] < '0' || upstream[0] > '9' {
			return fail("Version upstream")
		}
		if n := strings.LastIndexByte(upstream, '-'); n >= 0 {
			revision := upstream[n+1:]
			if revision == "" || strings.ContainsAny(revision, ":-") {
				return fail("Version revision")
			}
		}
		if !safe.Identifier(p.Architecture) {
			return fail("Architecture")
		}
		if !safe.Segment(p.Filename) || !strings.HasSuffix(p.Filename, ".deb") {
			return fail("Filename")
		}
		if p.Size <= 0 {
			return fail("Size")
		}
		if ok, _ := regexp.MatchString(`^[0-9a-f]{64}$`, p.SHA256); !ok {
			return fail("SHA256")
		}
		if !singleLine(p.Depends) {
			return fail("Depends")
		}
		lines := strings.Split(p.Description, "\n")
		if strings.TrimSpace(lines[0]) == "" || !singleLine(lines[0]) {
			return fail("Description")
		}
		for _, s := range lines[1:] {
			if !strings.HasPrefix(s, " ") || len(s) < 2 || !singleLine(s) {
				return fail("Description continuation")
			}
		}
		if tuples[p.Key()] || files[p.Filename] {
			return fail("duplicate package or asset")
		}
		tuples[p.Key()] = true
		files[p.Filename] = true
	}
	return nil
}
func singleLine(s string) bool {
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func (m Manifest) ValidateRelease(c config.Config, p projects.Project, tag string, assets []Asset) error {
	if e := m.Validate(); e != nil {
		return e
	}
	if m.Project != p.ID || m.ReleaseTag != tag {
		return errors.New("manifest does not belong to release")
	}
	byName := map[string]Asset{}
	ids := map[int64]bool{}
	for _, a := range assets {
		if a.ID <= 0 || a.Size <= 0 || !safe.Segment(a.Name) || ids[a.ID] {
			return errors.New("invalid asset metadata")
		}
		if _, ok := byName[a.Name]; ok {
			return errors.New("duplicate asset name")
		}
		ids[a.ID] = true
		byName[a.Name] = a
	}
	if _, ok := byName[p.Source.ManifestAsset]; !ok {
		return errors.New("manifest asset missing")
	}
	for _, record := range m.Packages {
		allowed := record.Architecture == "all"
		for _, a := range c.Repository.Architectures {
			allowed = allowed || a == record.Architecture
		}
		if !allowed {
			return errors.New("architecture not allowed")
		}
		a, ok := byName[record.Filename]
		if !ok || a.Size != record.Size {
			return errors.New("package asset missing or size mismatch")
		}
		if a.Digest != "" && a.Digest != "sha256:"+record.SHA256 {
			return errors.New("asset digest mismatch")
		}
		if _, e := p.MatchRule(record.Package, record.Architecture, record.Filename, tag); e != nil {
			return e
		}
	}
	return nil
}
