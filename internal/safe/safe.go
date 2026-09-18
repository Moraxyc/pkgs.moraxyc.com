package safe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var segment = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.+~\-]*$`)
var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
var pkg = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+$`)

func Segment(s string) bool    { return segment.MatchString(s) && !strings.Contains(s, "..") }
func Identifier(s string) bool { return identifier.MatchString(s) }
func Package(s string) bool    { return pkg.MatchString(s) }
func Pool(s string) bool {
	if !strings.HasPrefix(s, "pool/") || path.Clean(s) != s {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		if !Segment(p) {
			return false
		}
	}
	return true
}
func Regular(name string) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("expected regular file")
	}
	return os.ReadFile(name)
}
func Atomic(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(name), ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0644); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), name)
}
func JSON(data []byte, out any) error {
	if !utf8.Valid(data) {
		return errors.New("invalid UTF-8 JSON")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var scan func() error
	scan = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		if c, ok := t.(json.Delim); ok {
			switch c {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return e
					}
					s := k.(string)
					if seen[s] {
						return errors.New("duplicate JSON key")
					}
					seen[s] = true
					if e = scan(); e != nil {
						return e
					}
				}
			case '[':
				for d.More() {
					if e = scan(); e != nil {
						return e
					}
				}
			default:
				return errors.New("invalid JSON delimiter")
			}
			_, e = d.Token()
			return e
		}
		return nil
	}
	if err := scan(); err != nil {
		return errors.New("invalid or duplicate JSON fields")
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	d.UseNumber()
	if err := d.Decode(out); err != nil {
		return errors.New("invalid JSON shape")
	}
	return nil
}
func Command(input []byte, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	c.Stdin = bytes.NewReader(input)
	b, err := c.Output()
	if err != nil {
		return nil, errors.New(name + " failed")
	}
	return b, nil
}
