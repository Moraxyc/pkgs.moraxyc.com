package signing

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"pkgs.moraxyc.com/internal/safe"
	"regexp"
	"strconv"
	"strings"
)

var fingerprint = regexp.MustCompile(`^[A-F0-9]{40}$`)

func home() (string, error) {
	h, e := os.MkdirTemp("", "moraxyc-gpg-")
	if e == nil {
		e = os.Chmod(h, 0700)
	}
	return h, e
}
func cleanup(h string) {
	safe.Command(nil, "gpgconf", "--homedir", h, "--kill", "gpg-agent")
	os.RemoveAll(h)
}
func Public(key []byte, primary, sub string) error {
	return PublicAt(key, primary, sub, 0)
}

// PublicAt fixes key validity checks to the render epoch for offline replay.
// A zero epoch uses the current time for live signing and verification.
func PublicAt(key []byte, primary, sub string, epoch int64) error {
	if !fingerprint.MatchString(primary) || !fingerprint.MatchString(sub) || primary == sub {
		return errors.New("explicit primary and signing subkey fingerprints required")
	}
	// gpg accepts armor here, but gpgv --keyring requires a binary keyring.
	if bytes.HasPrefix(bytes.TrimSpace(key), []byte("-----BEGIN PGP")) {
		return errors.New("public keyring must use binary OpenPGP (.gpg), not ASCII armor")
	}
	h, e := home()
	if e != nil {
		return e
	}
	defer cleanup(h)
	args := []string{"--batch", "--no-options", "--homedir", h}
	if epoch > 0 {
		args = append(args, "--faked-system-time", strconv.FormatInt(epoch, 10)+"!")
	}
	args = append(args, "--with-colons", "--with-fingerprint", "--with-subkey-fingerprint", "--show-keys")
	b, e := safe.Command(key, "gpg", args...)
	if e != nil {
		return e
	}
	foundPrimary, foundSub := false, false
	kind := ""
	parent := ""
	valid := false
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 10 {
			continue
		}
		switch f[0] {
		case "sec", "ssb":
			return errors.New("keyring contains private key material")
		case "pub", "sub":
			kind = f[0]
			valid = f[1] != "r" && f[1] != "e"
			if kind == "sub" {
				valid = valid && len(f) > 11 && strings.Contains(f[11], "s")
			}
		case "fpr":
			if kind == "pub" {
				parent = f[9]
				foundPrimary = foundPrimary || valid && f[9] == primary
			}
			if kind == "sub" && parent == primary && valid && f[9] == sub {
				foundSub = true
			}
			kind = ""
		}
	}
	if !foundPrimary || !foundSub {
		return errors.New("public keyring does not contain expected usable primary and signing subkey")
	}
	return nil
}
func Verify(root string, key []byte, primary, sub string) error {
	if e := Public(key, primary, sub); e != nil {
		return e
	}
	h, e := home()
	if e != nil {
		return e
	}
	defer cleanup(h)
	keyPath := filepath.Join(h, "archive.gpg")
	if e = os.WriteFile(keyPath, key, 0600); e != nil {
		return e
	}
	base := filepath.Join(root, "deb/dists/stable")
	release, e := safe.Regular(filepath.Join(base, "Release"))
	if e != nil {
		return e
	}
	status, e := safe.Command(nil, "gpgv", "--homedir", h, "--keyring", keyPath, "--status-fd", "1", filepath.Join(base, "Release.gpg"), filepath.Join(base, "Release"))
	if e != nil {
		return e
	}
	if !validSignature(status, primary, sub) {
		return errors.New("unexpected detached signer")
	}
	clear := filepath.Join(h, "Release")
	status, e = safe.Command(nil, "gpgv", "--homedir", h, "--keyring", keyPath, "--status-fd", "1", "--output", clear, filepath.Join(base, "InRelease"))
	if e != nil {
		return e
	}
	if !validSignature(status, primary, sub) {
		return errors.New("unexpected clearsign signer")
	}
	plain, e := os.ReadFile(clear)
	if e != nil {
		return e
	}
	if !bytes.Equal(plain, release) {
		return errors.New("InRelease signs different Release content")
	}
	return nil
}
func validSignature(b []byte, primary, sub string) bool {
	count := 0
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 12 && f[0] == "[GNUPG:]" && f[1] == "VALIDSIG" {
			if f[2] != sub || f[len(f)-1] != primary {
				return false
			}
			count++
		}
	}
	return count == 1
}

// Sign reads only the supplied CI subkey material and never opens a user's keyring.
func Sign(root string, key, secret, passphrase []byte, primary, sub string) error {
	if e := Public(key, primary, sub); e != nil {
		return e
	}
	if len(secret) == 0 {
		return errors.New("CI signing subkey is missing")
	}
	h, e := home()
	if e != nil {
		return e
	}
	defer cleanup(h)
	// gpg --export-secret-subkeys leaves a primary secret key stub ('#').
	if _, e = safe.Command(secret, "gpg", "--batch", "--no-options", "--homedir", h, "--import"); e != nil {
		return fmt.Errorf("import CI signing subkey: %w", e)
	}
	b, e := safe.Command(nil, "gpg", "--batch", "--no-options", "--homedir", h, "--with-colons", "--list-secret-keys")
	if e != nil {
		return fmt.Errorf("inspect CI signing subkey: %w", e)
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) >= 15 && f[0] == "sec" && f[14] != "#" {
			return errors.New("CI must not contain a primary private key")
		}
	}
	base := filepath.Join(root, "deb/dists/stable")
	for _, name := range []string{"InRelease", "Release.gpg"} {
		if _, e = os.Lstat(filepath.Join(base, name)); !os.IsNotExist(e) {
			return errors.New("signature output already exists")
		}
	}
	// Attempt signing directly; gpg reports key unavailability as an error.
	for _, spec := range []struct{ name, mode string }{{"Release.gpg", "--detach-sign"}, {"InRelease", "--clearsign"}} {
		output := filepath.Join(h, spec.name)
		args := []string{"--batch", "--no-options", "--homedir", h, "--pinentry-mode", "loopback", "--passphrase-fd", "0", "--local-user", sub + "!", "--digest-algo", "SHA256", "--output", output, spec.mode, filepath.Join(base, "Release")}
		if _, e = safe.Command(passphrase, "gpg", args...); e != nil {
			return fmt.Errorf("sign %s: %w", spec.name, e)
		}
		data, e := os.ReadFile(output)
		if e != nil {
			return e
		}
		if e = safe.Atomic(filepath.Join(base, spec.name), data); e != nil {
			return e
		}
	}
	return Verify(root, key, primary, sub)
}
