package verify

import (
	"bytes"
	"errors"
	"io/fs"
	"path/filepath"
	"pkgs.moraxyc.com/internal/apt"
	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/safe"
	"pkgs.moraxyc.com/internal/signing"
	"pkgs.moraxyc.com/internal/snapshot"
)

func Tree(root string, c config.Config, rs []snapshot.Record, key []byte, epoch int64, primary, sub string, signed bool, history map[string]snapshot.HistoricalIndex) error {
	if e := signing.PublicAt(key, primary, sub, epoch); e != nil {
		return e
	}
	files, e := apt.PublicFiles(c, rs, key, epoch, history)
	if e != nil {
		return e
	}
	release, e := safe.Regular(filepath.Join(root, "deb/dists/stable/Release"))
	if e != nil {
		return e
	}
	if e = apt.CheckRelease(c, files, release, epoch); e != nil {
		return e
	}
	files["deb/dists/stable/Release"] = release
	if signed {
		for _, name := range []string{"InRelease", "Release.gpg"} {
			f := "deb/dists/stable/" + name
			files[f], e = safe.Regular(filepath.Join(root, f))
			if e != nil {
				return e
			}
		}
	}
	dirs := map[string]bool{".": true}
	for name := range files {
		for p := filepath.Dir(name); p != "."; p = filepath.Dir(p) {
			dirs[p] = true
		}
	}
	seen := map[string]bool{}
	e = filepath.WalkDir(root, func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, e := filepath.Rel(root, name)
		if e != nil {
			return e
		}
		if d.IsDir() {
			if !dirs[rel] {
				return errors.New("undeclared public directory")
			}
			return nil
		}
		expected, ok := files[rel]
		if !ok {
			return errors.New("undeclared file in public tree")
		}
		b, e := safe.Regular(name)
		if e != nil {
			return e
		}
		if !bytes.Equal(expected, b) {
			return errors.New("public file differs from validated input: " + rel)
		}
		seen[rel] = true
		return nil
	})
	if e != nil {
		return e
	}
	if len(seen) != len(files) {
		return errors.New("incomplete public tree")
	}
	if signed {
		return signing.Verify(root, key, primary, sub)
	}
	return nil
}
