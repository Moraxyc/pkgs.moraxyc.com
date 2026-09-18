package redirects

import (
	"errors"
	"net/url"
	"pkgs.moraxyc.com/internal/snapshot"
	"sort"
	"strings"
)

func Target(r snapshot.Record) string {
	parts := strings.Split(r.Repository, "/")
	parts = append(parts, "releases", "download", r.Tag, r.Package.Filename)
	for i, s := range parts {
		parts[i] = strings.ReplaceAll(url.PathEscape(s), "+", "%2B")
	}
	return "https://github.com/" + strings.Join(parts, "/")
}
func Render(records []snapshot.Record) ([]byte, error) {
	if len(records) > 2000 {
		return nil, errors.New("Pages static redirect limit exceeded (2000)")
	}
	lines := []string{}
	seen := map[string]bool{}
	for _, r := range records {
		if seen[r.Pool] {
			return nil, errors.New("redirect collision")
		}
		seen[r.Pool] = true
		line := "/deb/" + r.Pool + " " + Target(r) + " 302"
		if len(line) > 1000 {
			return nil, errors.New("Pages redirect line exceeds 1000 bytes")
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return []byte{}, nil
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

const Headers = "/deb/dists/*\n  Content-Type: text/plain; charset=utf-8\n  Cache-Control: public, max-age=300\n\n/deb/keys/*\n  Content-Type: application/pgp-keys\n  Cache-Control: public, max-age=300\n"

const NotFound = "<!doctype html><html lang=\"en\"><meta charset=\"utf-8\"><title>Not found</title><h1>Not found</h1></html>\n"
