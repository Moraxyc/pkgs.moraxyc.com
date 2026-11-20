package apt

import (
	"bytes"
	"html/template"
	"sort"
	"strings"

	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/snapshot"
)

type indexPackage struct {
	Name          string
	Architectures string
	Summary       string
	Repository    string
	SourceURL     string
}

type indexData struct {
	BaseURL        string
	Suite          string
	Component      string
	Architectures  string
	Organization   string
	GitHubKeysURL  string
	RepoURL        string
	KeyringURL     string
	ReleaseURL     string
	ExamplePackage string
	Packages       []indexPackage
}

var indexTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Moraxyc APT Repository · Moraxyc 软件包仓库</title>
<style>
:root{color-scheme:light dark;--bg:#fff;--fg:#1f2328;--muted:#59636e;--border:#d1d9e0;--code-bg:#f6f8fa;--link:#0969da}
@media(prefers-color-scheme:dark){:root{--bg:#0d1117;--fg:#e6edf3;--muted:#9198a1;--border:#30363d;--code-bg:#161b22;--link:#58a6ff}}
*{box-sizing:border-box}
body{margin:0;padding:2rem 1.25rem 3rem;background:var(--bg);color:var(--fg);font:15px/1.6 system-ui,-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif}
main{max-width:52rem;margin:0 auto}
h1{margin:0 0 .5rem;font-size:1.65rem;line-height:1.3}
h2{margin:2rem 0 .6rem;font-size:1.15rem;line-height:1.35;padding-bottom:.3rem;border-bottom:1px solid var(--border)}
p{margin:.5rem 0}
.muted{color:var(--muted);font-weight:400}
.zh{color:var(--muted);margin-top:.2rem}
code,pre{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.875em}
code{padding:.15em .35em;background:var(--code-bg);border-radius:4px}
pre{margin:.75rem 0;padding:.85rem 1rem;background:var(--code-bg);border:1px solid var(--border);border-radius:6px;overflow-x:auto;line-height:1.5}
pre code{padding:0;background:none}
table{width:100%;border-collapse:collapse;margin:.75rem 0;font-size:.92rem}
th,td{padding:.55rem .65rem;text-align:left;border-bottom:1px solid var(--border);vertical-align:top}
th{color:var(--muted);font-weight:600}
a{color:var(--link);text-decoration:none}
a:hover{text-decoration:underline}
footer{margin-top:2.5rem;padding-top:1rem;border-top:1px solid var(--border);color:var(--muted);font-size:.875rem}
</style>
</head>
<body>
<main>
<header>
<h1>Moraxyc APT Repository <span class="muted" lang="zh-CN">/ Moraxyc 软件包仓库</span></h1>
<p>APT repository for Debian and Ubuntu (<code>{{.Architectures}}</code>).</p>
<p class="zh" lang="zh-CN">适用于 Debian 和 Ubuntu 的 APT 软件包仓库，提供 <code>{{.Architectures}}</code> 架构软件包。</p>
</header>

<section>
<h2>1. Verify Signing Key <span class="muted" lang="zh-CN">/ 验证签名公钥</span></h2>
<p>Requires <code>curl</code>, <code>gpg</code>, and <code>sudo</code>. Compare the primary key fingerprint with <a href="{{.GitHubKeysURL}}">{{.GitHubKeysURL}}</a> before trusting the keyring:</p>
<p class="zh" lang="zh-CN">需要 <code>curl</code>、<code>gpg</code> 和管理员权限。导入前请先与 <a href="{{.GitHubKeysURL}}">GitHub 公钥</a> 核对主密钥指纹：</p>
<pre><code>curl -fsSL {{.KeyringURL}} -o /tmp/moraxyc-archive-keyring.gpg
gpg --show-keys --with-fingerprint /tmp/moraxyc-archive-keyring.gpg
curl -fsSL {{.GitHubKeysURL}} | gpg --show-keys --with-fingerprint</code></pre>
</section>

<section>
<h2>2. Add Repository <span class="muted" lang="zh-CN">/ 添加仓库</span></h2>
<p>Once the primary key fingerprint matches, install the keyring and add the APT source:</p>
<p class="zh" lang="zh-CN">确认主密钥指纹一致后，安装公钥环并添加 APT 软件源：</p>
<pre><code>sudo install -d -m 0755 /etc/apt/keyrings
sudo install -m 0644 /tmp/moraxyc-archive-keyring.gpg /etc/apt/keyrings/moraxyc-archive-keyring.gpg
printf '%s\n' 'deb [signed-by=/etc/apt/keyrings/moraxyc-archive-keyring.gpg] {{.BaseURL}} {{.Suite}} {{.Component}}' | sudo tee /etc/apt/sources.list.d/moraxyc.list
sudo apt-get update</code></pre>
</section>

<section>
<h2>3. Install Packages <span class="muted" lang="zh-CN">/ 安装软件</span></h2>
<p>Install a package (for example <code>{{.ExamplePackage}}</code>):</p>
<p class="zh" lang="zh-CN">安装仓库中的软件包（例如 <code>{{.ExamplePackage}}</code>）：</p>
<pre><code>sudo apt-get install {{.ExamplePackage}}</code></pre>
<p>If a package is not found, check that it is published for your architecture and re-run <code>sudo apt-get update</code>.</p>
<p class="zh" lang="zh-CN">如果找不到软件包，请确认该项目已发布适用于本机架构的版本，并重新运行 <code>sudo apt-get update</code>。</p>
</section>

<section>
<h2>4. Remove Repository <span class="muted" lang="zh-CN">/ 移除仓库</span></h2>
<p>Remove the repository list and keyring (installed packages are kept):</p>
<p class="zh" lang="zh-CN">移除仓库配置与公钥环（不会卸载已安装的软件包）：</p>
<pre><code>sudo rm -f /etc/apt/sources.list.d/moraxyc.list /etc/apt/keyrings/moraxyc-archive-keyring.gpg
sudo apt-get update</code></pre>
</section>

{{if .Packages}}
<section>
<h2>Packages <span class="muted" lang="zh-CN">/ 软件包</span></h2>
<table>
<thead>
<tr>
<th>Package <span class="muted" lang="zh-CN">/ 软件包</span></th>
<th>Arch <span class="muted" lang="zh-CN">/ 架构</span></th>
<th>Description <span class="muted" lang="zh-CN">/ 说明</span></th>
<th>Source <span class="muted" lang="zh-CN">/ 源码</span></th>
</tr>
</thead>
<tbody>
{{range .Packages}}
<tr>
<td><code>{{.Name}}</code></td>
<td><code>{{.Architectures}}</code></td>
<td>{{.Summary}}</td>
<td><a href="{{.SourceURL}}">{{.Repository}}</a></td>
</tr>
{{end}}
</tbody>
</table>
</section>
{{end}}

<footer>
<a href="{{.RepoURL}}">{{.Organization}}/pkgs.moraxyc.com</a>
· <a href="{{.KeyringURL}}">Keyring / 公钥环</a>
· <a href="{{.ReleaseURL}}">InRelease</a>
</footer>
</main>
</body>
</html>
`))

// IndexHTML renders the bilingual usage guide served at the repository root.
func IndexHTML(c config.Config, rs []snapshot.Record) ([]byte, error) {
	type pkgState struct {
		arches map[string]bool
		repo   string
		desc   string
	}
	byName := map[string]*pkgState{}
	for _, r := range rs {
		p := r.Package
		st, ok := byName[p.Package]
		if !ok {
			st = &pkgState{arches: map[string]bool{}}
			byName[p.Package] = st
		}
		st.arches[p.Architecture] = true
		st.repo = r.Repository
		st.desc = strings.SplitN(p.Description, "\n", 2)[0]
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	packages := make([]indexPackage, 0, len(names))
	for _, name := range names {
		st := byName[name]
		arches := make([]string, 0, len(st.arches))
		for arch := range st.arches {
			arches = append(arches, arch)
		}
		sort.Strings(arches)
		packages = append(packages, indexPackage{
			Name:          name,
			Architectures: strings.Join(arches, ", "),
			Summary:       st.desc,
			Repository:    st.repo,
			SourceURL:     "https://github.com/" + st.repo,
		})
	}

	example := "fetchrix"
	if len(packages) > 0 {
		example = packages[0].Name
	}

	baseURL := strings.TrimSuffix(c.Repository.BaseURL, "/")
	data := indexData{
		BaseURL:        baseURL,
		Suite:          c.Repository.Suite,
		Component:      strings.Join(c.Repository.Components, " "),
		Architectures:  strings.Join(c.Repository.Architectures, ", "),
		Organization:   c.GitHub.Organization,
		GitHubKeysURL:  "https://github.com/" + c.GitHub.Organization + ".gpg",
		RepoURL:        "https://github.com/" + c.GitHub.Organization + "/pkgs.moraxyc.com",
		KeyringURL:     baseURL + "/keys/moraxyc-archive-keyring.gpg",
		ReleaseURL:     baseURL + "/dists/" + c.Repository.Suite + "/InRelease",
		ExamplePackage: example,
		Packages:       packages,
	}

	var buf bytes.Buffer
	if err := indexTemplate.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
