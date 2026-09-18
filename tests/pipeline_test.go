package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"pkgs.moraxyc.com/internal/apt"
	"pkgs.moraxyc.com/internal/artifact"
	"pkgs.moraxyc.com/internal/redirects"
	"pkgs.moraxyc.com/internal/safe"
	"pkgs.moraxyc.com/internal/signing"
	"pkgs.moraxyc.com/internal/snapshot"
	"pkgs.moraxyc.com/internal/verify"
	"strconv"
	"strings"
	"testing"
	"time"
)

func syntheticKey(t *testing.T) ([]byte, []byte, string, string) {
	t.Helper()
	h := t.TempDir()
	must(t, os.Chmod(h, 0700))
	t.Cleanup(func() { safe.Command(nil, "gpgconf", "--homedir", h, "--kill", "gpg-agent") })
	args := []string{"--batch", "--no-options", "--homedir", h, "--pinentry-mode", "loopback", "--passphrase", ""}
	_, e := safe.Command(nil, "gpg", append(args, "--quick-generate-key", "Repository Test <test@example.invalid>", "rsa2048", "cert", "1d")...)
	must(t, e)
	list, e := safe.Command(nil, "gpg", "--batch", "--no-options", "--homedir", h, "--with-colons", "--list-keys")
	must(t, e)
	primary := ""
	for _, line := range strings.Split(string(list), "\n") {
		f := strings.Split(line, ":")
		if f[0] == "fpr" {
			primary = f[9]
			break
		}
	}
	if primary == "" {
		t.Fatal("missing synthetic primary")
	}
	_, e = safe.Command(nil, "gpg", append(args, "--quick-add-key", primary, "rsa2048", "sign", "1d")...)
	must(t, e)
	list, e = safe.Command(nil, "gpg", "--batch", "--no-options", "--homedir", h, "--with-colons", "--with-subkey-fingerprint", "--list-keys")
	must(t, e)
	sub := ""
	for _, line := range strings.Split(string(list), "\n") {
		f := strings.Split(line, ":")
		if f[0] == "fpr" && f[9] != primary {
			sub = f[9]
		}
	}
	key, e := safe.Command(nil, "gpg", "--batch", "--no-options", "--homedir", h, "--export", primary)
	must(t, e)
	secret, e := safe.Command(nil, "gpg", append(args, "--export-secret-subkeys", primary)...)
	must(t, e)
	return key, secret, primary, sub
}
func treeBytes(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	must(t, filepath.WalkDir(root, func(name string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		b, e := os.ReadFile(name)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, name)
		out[rel] = apt.Hash(b)
		return e
	}))
	return out
}
func TestSignedRepositoryAndTampering(t *testing.T) {
	c, ps, s := fixture(t)
	rs, e := s.Records(c, ps)
	must(t, e)
	key, secret, primary, sub := syntheticKey(t)
	epoch := time.Now().Unix()
	base := t.TempDir()
	one, two := filepath.Join(base, "one"), filepath.Join(base, "two")
	must(t, apt.Render(c, rs, key, epoch, one, nil))
	must(t, apt.Render(c, rs, key, epoch, two, nil))
	a, b := treeBytes(t, one), treeBytes(t, two)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatal("render is not deterministic")
	}
	for _, arch := range c.Repository.Architectures {
		data, e := os.ReadFile(filepath.Join(one, "deb/dists/stable/main/binary-"+arch+"/Packages"))
		must(t, e)
		if strings.Count(string(data), "Package: alpha\n") != 2 || strings.Count(string(data), "Package: beta\n") != 1 || !bytes.Contains(data, []byte("Architecture: all\n")) {
			t.Fatal("missing versions or all package")
		}
	}
	must(t, verify.Tree(one, c, rs, key, epoch, primary, sub, false, nil))
	releasePath := filepath.Join(one, "deb/dists/stable/Release")
	unsigned, e := os.ReadFile(releasePath)
	must(t, e)
	corruptedHash := append([]byte(nil), unsigned...)
	digestAt := bytes.Index(corruptedHash, []byte("SHA256:\n")) + len("SHA256:\n") + 1
	corruptedHash[digestAt] ^= 1
	must(t, os.WriteFile(releasePath, corruptedHash, 0644))
	if verify.Tree(one, c, rs, key, epoch, primary, sub, false, nil) == nil {
		t.Fatal("unsigned Release hash corruption accepted")
	}
	must(t, os.WriteFile(releasePath, unsigned, 0644))
	must(t, signing.Sign(one, key, secret, nil, primary, sub))
	must(t, verify.Tree(one, c, rs, key, epoch, primary, sub, true, nil))
	t.Run("retained indexes bridge deployments", func(t *testing.T) {
		history, err := apt.RetainIndexes(c, rs, nil, epoch)
		must(t, err)
		oldFiles, err := apt.Indexes(c, rs)
		must(t, err)
		changed := append([]snapshot.Record(nil), rs...)
		changed[0].Package.Version = "99.0"
		history, err = apt.RetainIndexes(c, changed, history, epoch+1)
		must(t, err)
		saved := s
		saved.IndexHistory = history
		snapshotPath := filepath.Join(t.TempDir(), "snapshot.json")
		must(t, saved.Save(snapshotPath))
		loaded, err := snapshot.Load(snapshotPath)
		must(t, err)
		next := filepath.Join(t.TempDir(), "next")
		must(t, apt.Render(c, changed, key, epoch+1, next, loaded.IndexHistory))
		must(t, verify.Tree(next, c, changed, key, epoch+1, primary, sub, false, loaded.IndexHistory))
		current, err := apt.Indexes(c, changed)
		must(t, err)
		oldOnly := ""
		for name, data := range oldFiles {
			if !strings.Contains(name, "/by-hash/") {
				continue
			}
			got, err := os.ReadFile(filepath.Join(next, name))
			must(t, err)
			if !bytes.Equal(got, data) {
				t.Fatal("historical index changed")
			}
			if _, ok := current[name]; !ok {
				oldOnly = name
			}
		}
		if oldOnly == "" {
			t.Fatal("fixture did not change an index")
		}
		must(t, safe.Atomic(filepath.Join(next, oldOnly), []byte("tampered")))
		if verify.Tree(next, c, changed, key, epoch+1, primary, sub, false, loaded.IndexHistory) == nil {
			t.Fatal("corrupted historical index accepted")
		}
		must(t, safe.Atomic(filepath.Join(next, oldOnly), oldFiles[oldOnly]))
		expiry := history[oldOnly].ExpiresAt
		before, err := apt.RetainIndexes(c, changed, history, expiry-1)
		must(t, err)
		if _, ok := before[oldOnly]; !ok {
			t.Fatal("historical index removed before expiry")
		}
		after, err := apt.RetainIndexes(c, changed, history, expiry)
		must(t, err)
		if _, ok := after[oldOnly]; ok {
			t.Fatal("expired historical index retained")
		}
		corrupted := history[oldOnly]
		corrupted.Data = []byte("wrong content")
		history[oldOnly] = corrupted
		if _, err := apt.PublicationIndexes(c, changed, history, epoch+1); err == nil {
			t.Fatal("history with incorrect digest accepted")
		}
		if os.Getenv("MORAXYC_INTEGRATION") == "1" {
			// Simulate fetching the old signed Release just before deployment
			// switches to the new tree and its retained by-hash indexes.
			for _, name := range []string{"Release", "InRelease", "Release.gpg"} {
				rel := filepath.Join("deb/dists/stable", name)
				data, err := os.ReadFile(filepath.Join(one, rel))
				must(t, err)
				must(t, safe.Atomic(filepath.Join(next, rel), data))
			}
			must(t, verify.APT(next, c, rs))
		}
	})
	t.Run("remote metadata and exact redirects", func(t *testing.T) {
		routeStatus := http.StatusFound
		fs := http.FileServer(http.Dir(one))
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if strings.HasPrefix(req.URL.Path, "/deb/pool/") {
				if req.URL.Path == "/deb/"+rs[0].Pool {
					w.Header().Set("Location", redirects.Target(rs[0]))
					w.WriteHeader(routeStatus)
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
				return
			}
			fs.ServeHTTP(w, req)
		}))
		defer server.Close()
		must(t, verify.Remote(context.Background(), server.URL, one, c, rs, nil, epoch))
		routeStatus = http.StatusMovedPermanently
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if verify.Remote(ctx, server.URL, one, c, rs, nil, epoch) == nil {
			t.Fatal("permanent pool redirect accepted")
		}
	})
	cases := []string{"_redirects", "deb/dists/stable/main/binary-amd64/Packages", "deb/dists/stable/Release", "deb/dists/stable/InRelease", "deb/dists/stable/Release.gpg"}
	for _, name := range cases {
		t.Run("tamper/"+name, func(t *testing.T) {
			file := filepath.Join(one, name)
			original, e := os.ReadFile(file)
			must(t, e)
			corrupted := append([]byte(nil), original...)
			corrupted[len(corrupted)/2] ^= 1
			must(t, os.WriteFile(file, corrupted, 0644))
			if verify.Tree(one, c, rs, key, epoch, primary, sub, true, nil) == nil {
				t.Fatal("tampering accepted")
			}
			must(t, os.WriteFile(file, original, 0644))
		})
	}
	bad := filepath.Join(one, "leaked.deb")
	must(t, os.WriteFile(bad, []byte("unexpected"), 0644))
	if verify.Tree(one, c, rs, key, epoch, primary, sub, true, nil) == nil {
		t.Fatal("extra file accepted")
	}
	must(t, os.Remove(bad))
	wrong, _, wp, ws := syntheticKey(t)
	if signing.Verify(one, wrong, wp, ws) == nil {
		t.Fatal("wrong signing key accepted")
	}
	if signing.Public(secret, primary, sub) == nil {
		t.Fatal("private keyring accepted as public")
	}
	if os.Getenv("MORAXYC_INTEGRATION") == "1" {
		t.Run("isolated APT client", func(t *testing.T) { must(t, verify.APT(one, c, rs)) })
	}
	if output := os.Getenv("MORAXYC_PREVIEW_DIR"); output != "" {
		if _, e := os.Lstat(output); !os.IsNotExist(e) {
			t.Fatal("preview output already exists")
		}
		must(t, os.CopyFS(output, os.DirFS(one)))
		context, e := json.MarshalIndent(map[string]any{"epoch": epoch, "primary_fingerprint": primary, "signing_fingerprint": sub}, "", "  ")
		must(t, e)
		must(t, safe.Atomic(output+"-context.json", append(context, '\n')))
	}
}
func TestProjectArtifactRoundTrip(t *testing.T) {
	base := t.TempDir()
	pkg := filepath.Join(base, "package")
	dist := filepath.Join(base, "dist")
	must(t, os.MkdirAll(filepath.Join(pkg, "DEBIAN"), 0755))
	must(t, os.MkdirAll(dist, 0755))
	control := "Package: sample\nVersion: 1.0-1\nArchitecture: all\nMaintainer: Test <test@example.invalid>\nDescription: A synthetic test package\n A second line\n"
	must(t, os.WriteFile(filepath.Join(pkg, "DEBIAN/control"), []byte(control), 0644))
	_, e := safe.Command(nil, "dpkg-deb", "--build", "--root-owner-group", pkg, filepath.Join(dist, "sample_1.0-1_all.deb"))
	must(t, e)
	m, e := artifact.Generate(dist, "sample", "v1.0")
	must(t, e)
	b, e := m.Bytes()
	must(t, e)
	manifestFile := filepath.Join(dist, "moraxyc-manifest.json")
	must(t, safe.Atomic(manifestFile, b))
	must(t, artifact.Verify(dist, "sample", "v1.0", []string{"amd64", "arm64"}))
	file := filepath.Join(dist, m.Packages[0].Filename)
	original, e := os.ReadFile(file)
	must(t, e)
	must(t, os.WriteFile(file, append(original, 'x'), 0644))
	if artifact.Verify(dist, "sample", "v1.0", []string{"amd64"}) == nil {
		t.Fatal("changed release artifact accepted")
	}
	must(t, os.WriteFile(file, original, 0644))
	bad := bytes.Replace(b, []byte(strconv.FormatInt(m.Packages[0].Size, 10)), []byte("1"), 1)
	must(t, os.WriteFile(manifestFile, bad, 0644))
	if artifact.Verify(dist, "sample", "v1.0", []string{"amd64"}) == nil {
		t.Fatal("changed manifest accepted")
	}
	must(t, os.WriteFile(manifestFile, b, 0644))
	must(t, os.Symlink(file, filepath.Join(dist, "unexpected.deb")))
	if artifact.Verify(dist, "sample", "v1.0", []string{"amd64"}) == nil {
		t.Fatal("unlisted symlink accepted")
	}
}
