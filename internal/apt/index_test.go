package apt

import (
	"bytes"
	"strings"
	"testing"

	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/manifest"
	"pkgs.moraxyc.com/internal/snapshot"
)

func testConfig() config.Config {
	var c config.Config
	c.Repository.BaseURL = "https://pkgs.moraxyc.com/deb"
	c.Repository.Suite = "stable"
	c.Repository.Codename = "stable"
	c.Repository.Components = []string{"main"}
	c.Repository.Architectures = []string{"amd64", "arm64"}
	c.Repository.ValidUntilDays = 7
	c.GitHub.Organization = "moraxyc"
	return c
}

func TestIndexHTMLBilingualGuideAndPackages(t *testing.T) {
	c := testConfig()
	rs := []snapshot.Record{
		{
			Package: manifest.Package{
				Package:      "whois42d-ng",
				Version:      "2.2.1-1",
				Architecture: "arm64",
				Description:  "WHOIS & RDAP <server>\n Second line",
			},
			Repository: "moraxyc/whois42d-ng",
		},
		{
			Package: manifest.Package{
				Package:      "fetchrix",
				Version:      "0.1.0-1",
				Architecture: "amd64",
				Description:  "Update package sources\n Long body",
			},
			Repository: "moraxyc/fetchrix",
		},
		{
			Package: manifest.Package{
				Package:      "whois42d-ng",
				Version:      "2.2.1-1",
				Architecture: "amd64",
				Description:  "WHOIS & RDAP <server>\n Second line",
			},
			Repository: "moraxyc/whois42d-ng",
		},
	}

	first, err := IndexHTML(c, rs)
	if err != nil {
		t.Fatal(err)
	}
	second, err := IndexHTML(c, rs)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("IndexHTML output is not deterministic")
	}

	html := string(first)
	for _, want := range []string{
		"Moraxyc APT Repository",
		"Moraxyc 软件包仓库",
		"1. Verify Signing Key",
		"验证签名公钥",
		"2. Add Repository",
		"添加仓库",
		"3. Install Packages",
		"安装软件",
		"4. Remove Repository",
		"移除仓库",
		"https://pkgs.moraxyc.com/deb/keys/moraxyc-archive-keyring.gpg",
		"https://github.com/moraxyc.gpg",
		"deb [signed-by=/etc/apt/keyrings/moraxyc-archive-keyring.gpg] https://pkgs.moraxyc.com/deb stable main",
		"sudo apt-get install fetchrix",
		"<code>amd64, arm64</code>",
		"WHOIS &amp; RDAP &lt;server&gt;",
		"https://github.com/moraxyc/fetchrix",
		"https://github.com/moraxyc/whois42d-ng",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing expected content %q in index.html", want)
		}
	}
	if strings.Contains(html, "Second line") {
		t.Fatal("index.html should only render the short summary line")
	}
}

func TestIndexHTMLEmptyRecords(t *testing.T) {
	c := testConfig()
	b, err := IndexHTML(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	if !strings.Contains(html, "sudo apt-get install fetchrix") {
		t.Fatal("expected fallback example package")
	}
	if strings.Contains(html, "<tbody>") {
		t.Fatal("empty records should not render packages table")
	}
}
