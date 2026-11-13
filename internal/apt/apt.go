package apt

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/redirects"
	"pkgs.moraxyc.com/internal/safe"
	"pkgs.moraxyc.com/internal/snapshot"
	"sort"
	"strconv"
	"strings"
	"time"
)

func Hash(b []byte) string    { return fmt.Sprintf("%x", sha256.Sum256(b)) }
func hash512(b []byte) string { return fmt.Sprintf("%x", sha512.Sum512(b)) }
func Packages(rs []snapshot.Record, arch string) []byte {
	var b strings.Builder
	for _, r := range rs {
		p := r.Package
		if p.Architecture != arch && p.Architecture != "all" {
			continue
		}
		fmt.Fprintf(&b, "Package: %s\nVersion: %s\nArchitecture: %s\nFilename: %s\nSize: %d\nSHA256: %s\n", p.Package, p.Version, p.Architecture, r.Pool, p.Size, p.SHA256)
		if p.Depends != "" {
			fmt.Fprintf(&b, "Depends: %s\n", p.Depends)
		}
		fmt.Fprintf(&b, "Description: %s\n\n", p.Description)
	}
	return []byte(b.String())
}
func Indexes(c config.Config, rs []snapshot.Record) (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, a := range c.Repository.Architectures {
		base := "deb/dists/stable/main/binary-" + a + "/"
		plain := Packages(rs, a)
		if len(plain) > 25<<20 {
			return nil, errors.New("index exceeds Pages 25 MiB file limit")
		}
		var buf bytes.Buffer
		z, e := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if e != nil {
			return nil, e
		}
		if _, e = z.Write(plain); e != nil {
			return nil, e
		}
		if e = z.Close(); e != nil {
			return nil, e
		}
		files[base+"Packages"] = plain
		files[base+"Packages.gz"] = buf.Bytes()
		files[base+"by-hash/SHA256/"+Hash(plain)] = plain
		files[base+"by-hash/SHA256/"+Hash(buf.Bytes())] = buf.Bytes()
		files[base+"by-hash/SHA512/"+hash512(plain)] = plain
		files[base+"by-hash/SHA512/"+hash512(buf.Bytes())] = buf.Bytes()
	}
	return files, nil
}
func Release(c config.Config, root string, epoch int64) ([]byte, error) {
	if epoch <= 0 || epoch > 253402214400 {
		return nil, errors.New("explicit valid epoch required")
	}
	date := time.Unix(epoch, 0).UTC()
	until := date.Add(time.Duration(c.Repository.ValidUntilDays) * 24 * time.Hour)
	args := []string{}
	opts := map[string]string{"Suite": c.Repository.Suite, "Codename": c.Repository.Codename, "Components": strings.Join(c.Repository.Components, " "), "Architectures": strings.Join(c.Repository.Architectures, " "), "Date": date.Format(time.RFC1123Z), "Acquire-By-Hash": "yes"}
	keys := []string{}
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-o", "APT::FTPArchive::Release::"+k+"="+opts[k])
	}
	args = append(args, "release", filepath.Join(root, "deb/dists/stable"))
	b, e := safe.Command(nil, "apt-ftparchive", args...)
	if e != nil {
		return nil, e
	}
	// apt-ftparchive does not emit Valid-Until on all supported versions.
	b = append([]byte("Valid-Until: "+until.Format(time.RFC1123Z)+"\n"), b...)
	return b, nil
}

func PublicFiles(c config.Config, rs []snapshot.Record, key []byte, epoch int64, history map[string]snapshot.HistoricalIndex) (map[string][]byte, error) {
	files, e := PublicationIndexes(c, rs, history, epoch)
	if e != nil {
		return nil, e
	}
	routes, e := redirects.Render(rs)
	if e != nil {
		return nil, e
	}
	files["_redirects"] = routes
	files["_headers"] = []byte(redirects.Headers)
	files["404.html"] = []byte(redirects.NotFound)
	files["deb/keys/moraxyc-archive-keyring.gpg"] = key
	return files, nil
}

func Render(c config.Config, rs []snapshot.Record, key []byte, epoch int64, out string, history map[string]snapshot.HistoricalIndex) error {
	if _, e := os.Lstat(out); e == nil {
		return errors.New("output already exists; choose a new build directory")
	} else if !os.IsNotExist(e) {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(out), 0755); e != nil {
		return e
	}
	tmp, e := os.MkdirTemp(filepath.Dir(out), ".render-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	files, e := PublicFiles(c, rs, key, epoch, history)
	if e != nil {
		return e
	}
	for name, b := range files {
		if e = safe.Atomic(filepath.Join(tmp, name), b); e != nil {
			return e
		}
	}
	release, e := Release(c, tmp, epoch)
	if e != nil {
		return e
	}
	if e = CheckRelease(c, files, release, epoch); e != nil {
		return e
	}
	if e = safe.Atomic(filepath.Join(tmp, "deb/dists/stable/Release"), release); e != nil {
		return e
	}
	return os.Rename(tmp, out)
}
func CheckRelease(c config.Config, files map[string][]byte, b []byte, epoch int64) error {
	fields := map[string]string{}
	sums := map[string]bool{}
	section := ""
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		if strings.HasPrefix(line, " ") {
			f := strings.Fields(line)
			if len(f) != 3 {
				return errors.New("malformed Release digest")
			}
			if section != "MD5Sum" && section != "SHA1" && section != "SHA256" && section != "SHA512" {
				return errors.New("unknown Release section")
			}
			name := "deb/dists/stable/" + f[2]
			data, ok := files[name]
			size, e := strconv.ParseInt(f[1], 10, 64)
			digest := Hash(data)
			switch section {
			case "MD5Sum":
				digest = fmt.Sprintf("%x", md5.Sum(data))
			case "SHA1":
				digest = fmt.Sprintf("%x", sha1.Sum(data))
			case "SHA512":
				digest = fmt.Sprintf("%x", sha512.Sum512(data))
			}
			sumKey := section + ":" + name
			if !ok || sums[sumKey] || e != nil || size != int64(len(data)) || f[0] != digest {
				return errors.New("Release digest or size mismatch")
			}
			sums[sumKey] = true
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return errors.New("malformed Release field")
		}
		if _, ok = fields[key]; ok {
			return errors.New("duplicate Release field")
		}
		fields[key] = strings.TrimSpace(value)
		section = key
	}
	expected := map[string]string{"Suite": c.Repository.Suite, "Codename": c.Repository.Codename, "Components": strings.Join(c.Repository.Components, " "), "Architectures": strings.Join(c.Repository.Architectures, " "), "Acquire-By-Hash": "yes", "Date": time.Unix(epoch, 0).UTC().Format(time.RFC1123Z), "Valid-Until": time.Unix(epoch, 0).UTC().Add(time.Duration(c.Repository.ValidUntilDays) * 24 * time.Hour).Format(time.RFC1123Z)}
	for k, v := range expected {
		if fields[k] != v {
			return errors.New("Release header mismatch: " + k)
		}
	}
	for _, a := range c.Repository.Architectures {
		for _, f := range []string{"Packages", "Packages.gz"} {
			if !sums["SHA256:deb/dists/stable/main/binary-"+a+"/"+f] {
				return errors.New("Release missing index")
			}
		}
	}
	return nil
}
