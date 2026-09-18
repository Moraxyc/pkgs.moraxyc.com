package verify

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/safe"
	"pkgs.moraxyc.com/internal/snapshot"
	"strings"
)

func APT(public string, c config.Config, records []snapshot.Record) error {
	server := httptest.NewServer(http.FileServer(http.Dir(public)))
	defer server.Close()
	state, e := os.MkdirTemp("", "moraxyc-apt-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(state)
	for _, dir := range []string{"lists/partial", "cache/archives/partial", "sources.list.d", "apt.conf.d", "trusted.gpg.d"} {
		if e := os.MkdirAll(filepath.Join(state, dir), 0755); e != nil {
			return e
		}
	}
	key, e := filepath.Abs(filepath.Join(public, "deb/keys/moraxyc-archive-keyring.gpg"))
	if e != nil {
		return e
	}
	source := filepath.Join(state, "sources.list")
	if e = os.WriteFile(source, []byte("deb [arch="+strings.Join(c.Repository.Architectures, ",")+" signed-by="+key+"] "+server.URL+"/deb stable main\n"), 0644); e != nil {
		return e
	}
	status := filepath.Join(state, "status")
	if e = os.WriteFile(status, nil, 0644); e != nil {
		return e
	}
	args := []string{"-o", "Dir::Etc=" + state, "-o", "Dir::Etc::main=/dev/null", "-o", "Dir::Etc::parts=" + filepath.Join(state, "apt.conf.d"), "-o", "Dir::Etc::sourcelist=" + source, "-o", "Dir::Etc::sourceparts=" + filepath.Join(state, "sources.list.d"), "-o", "Dir::Etc::trusted=/dev/null", "-o", "Dir::Etc::trustedparts=" + filepath.Join(state, "trusted.gpg.d"), "-o", "Dir::State=" + state, "-o", "Dir::State::status=" + status, "-o", "Dir::State::lists=" + filepath.Join(state, "lists"), "-o", "Dir::Cache=" + filepath.Join(state, "cache"), "-o", "Dir::Log=" + state, "-o", "APT::Architecture=" + c.Repository.Architectures[0], "-o", "Acquire::Languages=none", "-o", "Acquire::Retries=0", "-o", "Acquire::http::Timeout=10", "-o", "APT::Get::List-Cleanup=0"}
	for _, arch := range c.Repository.Architectures {
		args = append(args, "-o", "APT::Architectures::="+arch)
	}
	_, e = safe.Command(nil, "apt-get", append(args, "-o", "APT::Update::Error-Mode=any", "update")...)
	if e != nil {
		return e
	}
	for _, record := range records {
		p := record.Package
		selector := p.Package
		if p.Architecture != "all" {
			selector += ":" + p.Architecture
		}
		b, e := safe.Command(nil, "apt-cache", append(args, "show", selector+"="+p.Version)...)
		if e != nil {
			return e
		}
		fields := map[string]string{}
		for _, line := range strings.Split(string(b), "\n") {
			name, value, ok := strings.Cut(line, ": ")
			if ok {
				fields[name] = value
			}
		}
		if fields["Package"] != p.Package || fields["Version"] != p.Version || fields["Architecture"] != p.Architecture || fields["SHA256"] != p.SHA256 {
			return errors.New("APT metadata query differs from snapshot")
		}
	}
	return nil
}
