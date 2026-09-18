package projects

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/safe"
	"strings"
)

type Source struct {
	Repository         string `toml:"repository"`
	Enabled            bool   `toml:"enabled"`
	ManifestAsset      string `toml:"manifest_asset"`
	IncludePrereleases bool   `toml:"include_prereleases"`
}
type Rule struct {
	Package      string `toml:"package"`
	Architecture string `toml:"architecture"`
	Asset        string `toml:"asset"`
	Pool         string `toml:"pool"`
}
type Project struct {
	ID     string
	Source Source
	Rules  []Rule
}

func DiscoverProjects(root string, c config.Config) ([]Project, error) {
	entries, e := os.ReadDir(root)
	if e != nil {
		return nil, e
	}
	out := []Project{}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, errors.New("symlink in project registry")
		}
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		_, e := os.Lstat(filepath.Join(dir, "project.toml"))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		p := Project{ID: entry.Name()}
		var doc struct {
			Project Source `toml:"project"`
		}
		var mapping struct {
			Rules []Rule `toml:"rules"`
		}
		if e = config.Decode(filepath.Join(dir, "project.toml"), &doc, []string{"project", "repository"}, []string{"project", "enabled"}, []string{"project", "manifest_asset"}, []string{"project", "include_prereleases"}); e != nil {
			return nil, fmt.Errorf("project %s: %w", p.ID, e)
		}
		if e = config.Decode(filepath.Join(dir, "mapping.toml"), &mapping); e != nil {
			return nil, e
		}
		p.Source = doc.Project
		p.Rules = mapping.Rules
		if e = p.Validate(c); e != nil {
			return nil, fmt.Errorf("project %s: %w", p.ID, e)
		}
		out = append(out, p)
	}
	return out, nil
}
func (p Project) Validate(c config.Config) error {
	repo := strings.Split(p.Source.Repository, "/")
	if !safe.Identifier(p.ID) || len(repo) != 2 || repo[0] != c.GitHub.Organization || !safe.Segment(repo[1]) {
		return errors.New("invalid project ID or repository ownership")
	}
	if !safe.Segment(p.Source.ManifestAsset) || strings.HasSuffix(p.Source.ManifestAsset, ".deb") {
		return errors.New("invalid manifest asset")
	}
	if len(p.Rules) == 0 {
		return errors.New("empty mapping")
	}
	for _, r := range p.Rules {
		allowed := r.Architecture == "*" || r.Architecture == "all"
		for _, a := range c.Repository.Architectures {
			allowed = allowed || r.Architecture == a
		}
		if !safe.Package(r.Package) || !allowed || r.Asset == "" || strings.ContainsAny(r.Asset, "/\\\n\r\t ") {
			return errors.New("invalid mapping selector")
		}
		if _, e := path.Match(r.Asset, "test.deb"); e != nil {
			return errors.New("invalid asset glob")
		}
		v := strings.NewReplacer("{project}", p.ID, "{release_tag}", "v1", "{filename}", "sample.deb").Replace(r.Pool)
		if !safe.Pool(v) || !strings.HasSuffix(v, ".deb") {
			return errors.New("unsafe pool template")
		}
	}
	return nil
}
func (p Project) MatchRule(pkg, arch, asset, tag string) (string, error) {
	if !safe.Segment(asset) || !safe.Segment(tag) {
		return "", errors.New("unsafe asset or tag")
	}
	result := ""
	count := 0
	for _, r := range p.Rules {
		matched, _ := path.Match(r.Asset, asset)
		if r.Package == pkg && (r.Architecture == "*" || r.Architecture == arch) && matched {
			count++
			result = strings.NewReplacer("{project}", p.ID, "{release_tag}", tag, "{filename}", asset).Replace(r.Pool)
		}
	}
	if count != 1 || !safe.Pool(result) {
		return "", errors.New("record must have exactly one safe mapping")
	}
	return result, nil
}
