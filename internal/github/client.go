package github

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	github "github.com/google/go-github/v74/github"

	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/manifest"
	"pkgs.moraxyc.com/internal/projects"
	"pkgs.moraxyc.com/internal/safe"
	"pkgs.moraxyc.com/internal/snapshot"
)

type Client struct {
	HTTP      *http.Client
	BaseURL   string
	UploadURL string
	Token     string
}

func New(token string) *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 10 * time.Second}}, BaseURL: "https://api.github.com", Token: token}
}
func (c *Client) apiClient() (*github.Client, error) {
	if c.HTTP == nil {
		return nil, errors.New("invalid HTTP client")
	}
	base, err := url.Parse(strings.TrimSuffix(c.BaseURL, "/") + "/")
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("invalid API origin")
	}
	httpClient := *c.HTTP
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("API redirects not allowed")
	}
	api := github.NewClient(&httpClient)
	if c.Token != "" {
		api = api.WithAuthToken(c.Token)
	}
	api.BaseURL = base
	uploadURL := c.UploadURL
	if uploadURL == "" {
		if base.Scheme == "https" && base.Host == "api.github.com" {
			uploadURL = "https://uploads.github.com/"
		} else {
			uploadURL = base.String()
		}
	}
	upload, err := url.Parse(strings.TrimSuffix(uploadURL, "/") + "/")
	if err != nil || upload.Host == "" || upload.User != nil || upload.RawQuery != "" || upload.Fragment != "" {
		return nil, errors.New("invalid upload origin")
	}
	api.UploadURL = upload
	return api, nil
}

type API struct {
	client         *github.Client
	downloadClient *http.Client
}

// API keeps GitHub authentication separate from redirected asset downloads.
func (c *Client) API() (*API, error) {
	api, err := c.apiClient()
	if err != nil {
		return nil, err
	}
	return &API{client: api, downloadClient: c.HTTP}, nil
}

type Release struct {
	ID         int64
	Tag        string
	Draft      bool
	Prerelease bool
}

type ReleaseAsset struct {
	ID    int64
	Name  string
	Size  int64
	State string
}

func releaseValue(r *github.RepositoryRelease) (*Release, error) {
	if r == nil || r.GetID() <= 0 {
		return nil, errors.New("invalid release metadata")
	}
	return &Release{ID: r.GetID(), Tag: r.GetTagName(), Draft: r.GetDraft(), Prerelease: r.GetPrerelease()}, nil
}

func notFound(err error) bool {
	var response *github.ErrorResponse
	return errors.As(err, &response) && response.Response != nil && response.Response.StatusCode == http.StatusNotFound
}

func githubOperationError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New("GitHub client operation failed")
}

func (a *API) Commit(ctx context.Context, owner, repo, ref string) (string, error) {
	commit, _, err := a.client.Repositories.GetCommit(ctx, owner, repo, ref, nil)
	if err != nil {
		return "", githubOperationError(err)
	}
	if commit == nil || commit.GetSHA() == "" {
		return "", errors.New("invalid commit metadata")
	}
	return commit.GetSHA(), nil
}

// Release includes drafts visible to the authenticated publisher.
func (a *API) Release(ctx context.Context, owner, repo, tag string) (*Release, error) {
	release, _, err := a.client.Repositories.GetReleaseByTag(ctx, owner, repo, tag)
	if err != nil {
		if notFound(err) {
			var found *Release
			err = list(func(opts *github.ListOptions) ([]*github.RepositoryRelease, *github.Response, error) {
				return a.client.Repositories.ListReleases(ctx, owner, repo, opts)
			}, func(r *github.RepositoryRelease) error {
				if r.GetTagName() != tag {
					return nil
				}
				if found != nil {
					return errors.New("duplicate releases for tag")
				}
				var e error
				found, e = releaseValue(r)
				return e
			})
			return found, err
		}
		return nil, githubOperationError(err)
	}
	return releaseValue(release)
}

func (a *API) CreateRelease(ctx context.Context, owner, repo, tag string, prerelease bool) (*Release, error) {
	release, _, err := a.client.Repositories.CreateRelease(ctx, owner, repo, &github.RepositoryRelease{
		TagName:    github.String(tag),
		Name:       github.String(tag),
		Body:       github.String(""),
		Draft:      github.Bool(true),
		Prerelease: github.Bool(prerelease),
	})
	if err != nil {
		return nil, githubOperationError(err)
	}
	return releaseValue(release)
}

func (a *API) PublishRelease(ctx context.Context, owner, repo string, id int64) error {
	_, _, err := a.client.Repositories.EditRelease(ctx, owner, repo, id, &github.RepositoryRelease{Draft: github.Bool(false)})
	return githubOperationError(err)
}

func (a *API) ListReleaseAssets(ctx context.Context, owner, repo string, releaseID int64) ([]ReleaseAsset, error) {
	assets := []ReleaseAsset{}
	err := list(func(opts *github.ListOptions) ([]*github.ReleaseAsset, *github.Response, error) {
		return a.client.Repositories.ListReleaseAssets(ctx, owner, repo, releaseID, opts)
	}, func(asset *github.ReleaseAsset) error {
		if asset.GetID() <= 0 || asset.GetName() == "" || asset.GetSize() < 0 {
			return errors.New("invalid Release asset metadata")
		}
		size := int64(asset.GetSize())
		if asset.Size == nil {
			size = -1
		}
		assets = append(assets, ReleaseAsset{ID: asset.GetID(), Name: asset.GetName(), Size: size, State: asset.GetState()})
		return nil
	})
	return assets, err
}

// DeleteFailedAsset removes only an upload placeholder identified by the caller.
func (a *API) DeleteFailedAsset(ctx context.Context, owner, repo string, id int64) error {
	_, err := a.client.Repositories.DeleteReleaseAsset(ctx, owner, repo, id)
	return githubOperationError(err)
}

func (a *API) UploadReleaseAsset(ctx context.Context, owner, repo string, releaseID int64, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) || name == "" {
		return errors.New("invalid Release asset name")
	}
	_, _, err = a.client.Repositories.UploadReleaseAsset(ctx, owner, repo, releaseID, &github.UploadOptions{Name: name}, file)
	return githubOperationError(err)
}

func (a *API) assetReader(ctx context.Context, owner, repo string, id int64) (io.ReadCloser, error) {
	body, redirect, err := a.client.Repositories.DownloadReleaseAsset(ctx, owner, repo, id, nil)
	if err != nil {
		return nil, errors.New("GitHub asset request failed")
	}
	if redirect == "" {
		return body, nil
	}
	validate := func(u *url.URL) error {
		if u.Scheme != "https" || u.User != nil || (u.Host != a.client.BaseURL.Host && u.Host != "release-assets.githubusercontent.com" && u.Host != "objects.githubusercontent.com") {
			return errors.New("unsafe asset redirect")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, redirect, nil)
	if err != nil {
		return nil, errors.New("invalid asset redirect")
	}
	if err = validate(req.URL); err != nil {
		return nil, err
	}
	client := *a.downloadClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return validate(req.URL)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("GitHub asset download failed")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub HTTP status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// DownloadReleaseAsset writes an asset to path and checks the API-reported
// size when it is non-negative.
func (a *API) DownloadReleaseAsset(ctx context.Context, owner, repo string, id int64, path string, expectedSize int64) error {
	body, err := a.assetReader(ctx, owner, repo, id)
	if err != nil {
		return err
	}
	defer body.Close()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	reader := io.Reader(body)
	if expectedSize >= 0 {
		reader = io.LimitReader(body, expectedSize+1)
	}
	n, err := io.Copy(file, reader)
	if err != nil {
		return errors.New("GitHub asset download failed")
	}
	if expectedSize >= 0 && n != expectedSize {
		return errors.New("GitHub asset size mismatch")
	}
	if err = file.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}

func (a *API) Dispatch(ctx context.Context, owner, repo, event string, payload []byte) error {
	data := json.RawMessage(append([]byte(nil), payload...))
	_, _, err := a.client.Repositories.Dispatch(ctx, owner, repo, github.DispatchRequestOptions{EventType: event, ClientPayload: &data})
	return githubOperationError(err)
}

func list[T any](fetch func(*github.ListOptions) ([]*T, *github.Response, error), visit func(*T) error) error {
	opts := &github.ListOptions{PerPage: 100, Page: 1}
	for pages := 0; pages < 1000; pages++ {
		rows, resp, err := fetch(opts)
		if err != nil {
			return errors.New("GitHub list request failed")
		}
		if rows == nil {
			return errors.New("invalid API list")
		}
		if resp == nil {
			return errors.New("invalid API response")
		}
		for _, row := range rows {
			if row == nil {
				return errors.New("invalid API list entry")
			}
			if err := visit(row); err != nil {
				return err
			}
		}
		if resp.NextPage == 0 {
			return nil
		}
		if resp.NextPage <= opts.Page {
			return errors.New("pagination cycle")
		}
		opts.Page = resp.NextPage
	}
	return errors.New("pagination limit")
}

func (c *Client) download(ctx context.Context, api *github.Client, owner, repo string, id int64) ([]byte, error) {
	body, err := (&API{client: api, downloadClient: c.HTTP}).assetReader(ctx, owner, repo, id)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	b, err := io.ReadAll(io.LimitReader(body, 1<<20+1))
	if err != nil || len(b) > 1<<20 {
		return nil, errors.New("manifest response unreadable or oversized")
	}
	return b, nil
}

func (c *Client) downloadManifest(ctx context.Context, api *github.Client, owner, repo string, asset manifest.Asset) ([]byte, error) {
	if asset.ID <= 0 || asset.Size <= 0 || asset.Size > 1<<20 {
		return nil, errors.New("manifest missing or invalid size")
	}
	b, err := c.download(ctx, api, owner, repo, asset.ID)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) != asset.Size {
		return nil, errors.New("manifest asset size mismatch")
	}
	if asset.Digest != "" && asset.Digest != fmt.Sprintf("sha256:%x", sha256.Sum256(b)) {
		return nil, errors.New("manifest asset digest mismatch")
	}
	return b, nil
}

func (c *Client) collectRelease(ctx context.Context, cfg config.Config, p projects.Project, api *github.Client, owner, repo string, api_release *github.RepositoryRelease) (snapshot.Release, bool, error) {
	if api_release.Draft == nil || api_release.Prerelease == nil || api_release.GetID() <= 0 {
		return snapshot.Release{}, false, errors.New("invalid release metadata")
	}
	if *api_release.Draft || *api_release.Prerelease && !p.Source.IncludePrereleases {
		return snapshot.Release{}, true, nil
	}
	if api_release.PublishedAt == nil {
		return snapshot.Release{}, false, errors.New("missing release publication time")
	}
	if !safe.Segment(api_release.GetTagName()) {
		return snapshot.Release{}, false, errors.New("unsafe release tag")
	}
	r := snapshot.Release{
		ID:          api_release.GetID(),
		Tag:         api_release.GetTagName(),
		Prerelease:  *api_release.Prerelease,
		PublishedAt: api_release.GetPublishedAt().UTC().Format(time.RFC3339),
		Assets:      []manifest.Asset{},
	}
	err := list(func(opts *github.ListOptions) ([]*github.ReleaseAsset, *github.Response, error) {
		return api.Repositories.ListReleaseAssets(ctx, owner, repo, api_release.GetID(), opts)
	}, func(a *github.ReleaseAsset) error {
		r.Assets = append(r.Assets, manifest.Asset{ID: a.GetID(), Name: a.GetName(), Size: int64(a.GetSize()), Digest: a.GetDigest()})
		return nil
	})
	if err != nil {
		return r, false, err
	}
	var selected *manifest.Asset
	for i := range r.Assets {
		if r.Assets[i].Name == p.Source.ManifestAsset {
			if selected != nil {
				return r, false, errors.New("duplicate manifest asset")
			}
			selected = &r.Assets[i]
		}
	}
	if selected == nil {
		return r, true, nil
	}
	b, err := c.downloadManifest(ctx, api, owner, repo, *selected)
	if err != nil {
		return r, false, err
	}
	r.Manifest, err = manifest.Parse(b)
	if err != nil {
		return r, false, err
	}
	if err = r.Manifest.ValidateRelease(cfg, p, r.Tag, r.Assets); err != nil {
		return r, false, err
	}
	return r, false, nil
}

func (c *Client) collectProject(ctx context.Context, cfg config.Config, p projects.Project, api *github.Client) (snapshot.Project, error) {
	owner, repo, _ := strings.Cut(p.Source.Repository, "/")
	sp := snapshot.Project{ID: p.ID, Repository: p.Source.Repository, Releases: []snapshot.Release{}}
	err := list(func(opts *github.ListOptions) ([]*github.RepositoryRelease, *github.Response, error) {
		return api.Repositories.ListReleases(ctx, owner, repo, opts)
	}, func(api_release *github.RepositoryRelease) error {
		r, skip, e := c.collectRelease(ctx, cfg, p, api, owner, repo, api_release)
		if e != nil {
			return fmt.Errorf("release %d tag %q: %w", api_release.GetID(), api_release.GetTagName(), e)
		}
		if skip {
			return nil
		}
		sp.Releases = append(sp.Releases, r)
		return nil
	})
	return sp, err
}

func (c *Client) Collect(ctx context.Context, cfg config.Config, ps []projects.Project, now time.Time) (snapshot.Snapshot, error) {
	s := snapshot.Snapshot{Schema: 1, Projects: []snapshot.Project{}}
	s.Collection.CompletedAt = now.UTC().Format(time.RFC3339)
	if e := cfg.Validate(); e != nil {
		return s, e
	}
	for _, p := range ps {
		if e := p.Validate(cfg); e != nil {
			return s, e
		}
	}
	apiClient, err := c.apiClient()
	if err != nil {
		return s, err
	}
	for _, p := range ps {
		if !p.Source.Enabled {
			continue
		}
		sp, err := c.collectProject(ctx, cfg, p, apiClient)
		if err != nil {
			return s, fmt.Errorf("project %s: %w", p.ID, err)
		}
		s.Projects = append(s.Projects, sp)
	}
	if _, e := s.Records(cfg, ps); e != nil {
		return s, e
	}
	s.Normalize()
	return s, nil
}
