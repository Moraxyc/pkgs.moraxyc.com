package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"pkgs.moraxyc.com/internal/manifest"
	"pkgs.moraxyc.com/internal/safe"
	"reflect"
	"strings"
)

func readPackage(name string) (manifest.Package, error) {
	var p manifest.Package
	info, e := os.Lstat(name)
	if e != nil {
		return p, e
	}
	if !info.Mode().IsRegular() || !safe.Segment(filepath.Base(name)) || !strings.HasSuffix(name, ".deb") {
		return p, errors.New("expected regular .deb asset")
	}
	p.Filename = filepath.Base(name)
	p.Size = info.Size()
	f, e := os.Open(name)
	if e != nil {
		return p, e
	}
	h := sha256.New()
	_, e = io.Copy(h, f)
	f.Close()
	if e != nil {
		return p, e
	}
	p.SHA256 = hex.EncodeToString(h.Sum(nil))
	for _, field := range []struct {
		name   string
		target *string
	}{{"Package", &p.Package}, {"Version", &p.Version}, {"Architecture", &p.Architecture}, {"Depends", &p.Depends}, {"Description", &p.Description}} {
		b, e := safe.Command(nil, "dpkg-deb", "-f", name, field.name)
		if e != nil {
			return p, e
		}
		*field.target = strings.TrimSuffix(string(b), "\n")
	}
	if _, e = safe.Command(nil, "dpkg-deb", "--info", name); e != nil {
		return p, e
	}
	if _, e = safe.Command(nil, "dpkg-deb", "--contents", name); e != nil {
		return p, e
	}
	return p, nil
}
func Generate(dir, project, tag string) (manifest.Manifest, error) {
	m := manifest.Manifest{Schema: 1, Project: project, ReleaseTag: tag, Packages: []manifest.Package{}}
	entries, e := os.ReadDir(dir)
	if e != nil {
		return m, e
	}
	for _, entry := range entries {
		if entry.Name() == "moraxyc-manifest.json" {
			continue
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".deb") {
			return m, errors.New("unexpected build artifact")
		}
		p, e := readPackage(filepath.Join(dir, entry.Name()))
		if e != nil {
			return m, e
		}
		m.Packages = append(m.Packages, p)
	}
	b, e := m.Bytes()
	if e != nil {
		return m, e
	}
	return manifest.Parse(b)
}
func Verify(dir, project, tag string, architectures []string) error {
	b, e := safe.Regular(filepath.Join(dir, "moraxyc-manifest.json"))
	if e != nil {
		return e
	}
	expected, e := manifest.Parse(b)
	if e != nil {
		return e
	}
	if expected.Project != project || expected.ReleaseTag != tag {
		return errors.New("artifact manifest identity mismatch")
	}
	actual, e := Generate(dir, project, tag)
	if e != nil {
		return e
	}
	expected.Normalize()
	actual.Normalize()
	if !reflect.DeepEqual(expected, actual) {
		return errors.New("artifact bytes or Debian fields differ from manifest")
	}
	for _, p := range actual.Packages {
		allowed := p.Architecture == "all"
		for _, a := range architectures {
			allowed = allowed || p.Architecture == a
		}
		if !allowed {
			return errors.New("artifact architecture not allowed")
		}
	}
	return nil
}
