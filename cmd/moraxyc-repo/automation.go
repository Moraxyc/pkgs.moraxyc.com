package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"pkgs.moraxyc.com/internal/apt"
	"pkgs.moraxyc.com/internal/artifact"
	"pkgs.moraxyc.com/internal/config"
	githubclient "pkgs.moraxyc.com/internal/github"
	"pkgs.moraxyc.com/internal/projects"
	"pkgs.moraxyc.com/internal/safe"
	"pkgs.moraxyc.com/internal/snapshot"
)

func artifactHashes(dir string) (map[string][32]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	hashes := map[string][32]byte{}
	for _, entry := range entries {
		data, err := safe.Regular(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		hashes[entry.Name()] = sha256.Sum256(data)
	}
	return hashes, nil
}

type projectCIOptions struct {
	directory     string
	project       string
	tag           string
	repository    string
	commit        string
	architectures string
	prerelease    bool
}

func defaultProjectCIOptions() projectCIOptions {
	return projectCIOptions{
		directory:     "dist",
		project:       os.Getenv("PROJECT_ID"),
		tag:           os.Getenv("RELEASE_TAG"),
		repository:    os.Getenv("PROJECT_REPOSITORY"),
		commit:        os.Getenv("TESTED_COMMIT"),
		architectures: "amd64,arm64",
	}
}

func newProjectCICommand() *cobra.Command {
	opts := defaultProjectCIOptions()
	command := &cobra.Command{
		Use:   "project-ci",
		Short: "Validate and publish project releases",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("project-ci operation required")
		},
	}
	flags := command.PersistentFlags()
	flags.StringVar(&opts.directory, "artifacts", opts.directory, "artifact directory")
	flags.StringVar(&opts.project, "project", opts.project, "project ID")
	flags.StringVar(&opts.tag, "tag", opts.tag, "release tag")
	flags.StringVar(&opts.repository, "repository", opts.repository, "project repository")
	flags.StringVar(&opts.commit, "commit", opts.commit, "exact tested commit")
	flags.StringVar(&opts.architectures, "architectures", opts.architectures, "accepted architectures")
	flags.BoolVar(&opts.prerelease, "prerelease", opts.prerelease, "publish a prerelease")
	for _, operation := range []struct {
		name  string
		short string
	}{
		{name: "manifest", short: "Generate and verify the project artifact manifest"},
		{name: "verify-artifacts", short: "Verify project artifacts"},
		{name: "notify", short: "Notify the central repository about a published release"},
		{name: "publish", short: "Publish and round-trip verify a project release"},
		{name: "verify", short: "Round-trip verify an existing project release"},
	} {
		name, short := operation.name, operation.short
		child := &cobra.Command{
			Use:   name,
			Short: short,
			Args:  cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				return executeProjectCI(name, opts)
			},
		}
		command.AddCommand(child)
	}
	return command
}

// projectCI is retained as a small compatibility wrapper for callers that
// used the old entry point directly. The actual argument parsing is handled by
// the Cobra command tree.
func projectCI(args []string) error {
	command := newProjectCICommand()
	command.SetArgs(args)
	return command.Execute()
}

func projectCIToken() string {
	if token := os.Getenv("GH_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("GITHUB_TOKEN")
}

func projectCIClient() *githubclient.Client {
	client := githubclient.New(projectCIToken())
	if baseURL := os.Getenv("GITHUB_API_URL"); baseURL != "" {
		client.BaseURL = baseURL
	}
	if uploadURL := os.Getenv("GITHUB_UPLOAD_URL"); uploadURL != "" {
		client.UploadURL = uploadURL
	}
	return client
}

func executeProjectCI(operation string, opts projectCIOptions) error {
	if operation == "manifest" || operation == "verify-artifacts" {
		return executeArtifact(operation, artifactOptions{
			directory:     opts.directory,
			project:       opts.project,
			tag:           opts.tag,
			architectures: opts.architectures,
		})
	}
	if !regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9_.-]+$`).MatchString(opts.repository) || !safe.Identifier(opts.project) || !safe.Segment(opts.tag) {
		return errors.New("invalid project release identity")
	}
	if operation == "notify" {
		return dispatchNotification(opts)
	}
	if operation != "publish" && operation != "verify" {
		return errors.New("unknown project-ci operation")
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(opts.commit) {
		return errors.New("an exact tested commit is required")
	}
	return publishOrVerify(operation, opts)
}

func dispatchNotification(opts projectCIOptions) error {
	payload, err := json.Marshal(map[string]string{"project": opts.project, "repository": opts.repository, "release_tag": opts.tag, "manifest_asset": "moraxyc-manifest.json"})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	client, err := projectCIClient().API()
	if err != nil {
		return err
	}
	return client.Dispatch(ctx, "moraxyc", "pkgs.moraxyc.com", "project-release-published", payload)
}

func publishOrVerify(operation string, opts projectCIOptions) error {
	validate := func(directory string) error {
		return artifact.Verify(directory, opts.project, opts.tag, strings.Split(opts.architectures, ","))
	}
	if err := validate(opts.directory); err != nil {
		return err
	}
	expected, err := artifactHashes(opts.directory)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	client, err := projectCIClient().API()
	if err != nil {
		return err
	}
	owner, repository, _ := strings.Cut(opts.repository, "/")

	if err = checkCommit(ctx, client, owner, repository, opts); err != nil {
		return err
	}
	current, err := ensureRelease(ctx, client, owner, repository, opts, operation)
	if err != nil {
		return err
	}
	if operation == "publish" {
		if err = uploadAssets(ctx, client, owner, repository, current, opts, expected); err != nil {
			return err
		}
	}
	if err = roundTripVerify(ctx, client, owner, repository, current, opts, expected, validate); err != nil {
		return err
	}
	if current.Draft {
		if err = finalizeRelease(ctx, client, owner, repository, current, opts); err != nil {
			return err
		}
	}
	return nil
}

func checkCommit(ctx context.Context, client *githubclient.API, owner, repo string, opts projectCIOptions) error {
	actual, err := client.Commit(ctx, owner, repo, opts.tag)
	if err != nil {
		return err
	}
	if actual != opts.commit {
		return errors.New("release tag does not point to tested commit")
	}
	return nil
}

func ensureRelease(ctx context.Context, client *githubclient.API, owner, repo string, opts projectCIOptions, operation string) (*githubclient.Release, error) {
	current, err := client.Release(ctx, owner, repo, opts.tag)
	if err != nil {
		return nil, err
	}
	if current != nil {
		if current.Draft && operation != "publish" || current.Tag != opts.tag || current.Prerelease != opts.prerelease {
			return nil, errors.New("release identity or state mismatch")
		}
		return current, nil
	}
	if operation != "publish" {
		return nil, errors.New("Release is missing")
	}
	return client.CreateRelease(ctx, owner, repo, opts.tag, opts.prerelease)
}

func uploadAssets(ctx context.Context, client *githubclient.API, owner, repo string, current *githubclient.Release, opts projectCIOptions, expected map[string][32]byte) error {
	assets, err := client.ListReleaseAssets(ctx, owner, repo, current.ID)
	if err != nil {
		return err
	}
	seen := map[string]githubclient.ReleaseAsset{}
	for _, asset := range assets {
		if _, ok := expected[asset.Name]; !ok || seen[asset.Name].ID != 0 {
			return errors.New("Release asset set differs from tested artifact set")
		}
		if asset.State == "starter" {
			if err = client.DeleteFailedAsset(ctx, owner, repo, asset.ID); err != nil {
				return err
			}
			continue
		}
		seen[asset.Name] = asset
	}
	names := make([]string, 0, len(expected))
	for name := range expected {
		if _, ok := seen[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err = client.UploadReleaseAsset(ctx, owner, repo, current.ID, filepath.Join(opts.directory, name)); err != nil {
			return err
		}
	}
	return nil
}

func roundTripVerify(ctx context.Context, client *githubclient.API, owner, repo string, current *githubclient.Release, opts projectCIOptions, expected map[string][32]byte, validate func(string) error) error {
	assets, err := client.ListReleaseAssets(ctx, owner, repo, current.ID)
	if err != nil {
		return err
	}
	seen := map[string]githubclient.ReleaseAsset{}
	for _, asset := range assets {
		if _, ok := expected[asset.Name]; !ok || seen[asset.Name].ID != 0 {
			return errors.New("Release asset set differs from tested artifact set")
		}
		seen[asset.Name] = asset
	}
	if len(seen) != len(expected) {
		return errors.New("Release asset set differs from tested artifact set")
	}
	target, err := os.MkdirTemp("", "moraxyc-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(target)
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		asset := seen[name]
		if err = client.DownloadReleaseAsset(ctx, owner, repo, asset.ID, filepath.Join(target, name), asset.Size); err != nil {
			return err
		}
	}
	if err = validate(target); err != nil {
		return err
	}
	actualHashes, err := artifactHashes(target)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actualHashes, expected) {
		return errors.New("Release bytes differ from tested artifacts")
	}
	return nil
}

func finalizeRelease(ctx context.Context, client *githubclient.API, owner, repo string, current *githubclient.Release, opts projectCIOptions) error {
	// Re-check commit before publishing to guard against tag moves between
	// the initial check and the publish step.
	if err := checkCommit(ctx, client, owner, repo, opts); err != nil {
		return err
	}
	if err := client.PublishRelease(ctx, owner, repo, current.ID); err != nil {
		return err
	}
	published, err := client.Release(ctx, owner, repo, opts.tag)
	if err != nil {
		return err
	}
	if published == nil || published.Draft {
		return errors.New("Release publication was not confirmed")
	}
	return nil
}

func newCentralCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "central",
		Short: "Run central publication operations",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("central requires one operation")
		},
	}
	for _, operation := range []struct {
		name  string
		short string
	}{
		{name: "deploy", short: "Deploy the rendered repository to Pages"},
		{name: "sign", short: "Sign the rendered repository using publication context"},
		{name: "apt-smoke", short: "Run the central APT smoke test"},
		{name: "smoke", short: "Run the central remote smoke test"},
	} {
		name, short := operation.name, operation.short
		command.AddCommand(&cobra.Command{
			Use:   name,
			Short: short,
			Args:  cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				return executeCentral(name)
			},
		})
	}
	return command
}

// central is retained as a compatibility wrapper for callers that used the
// old entry point directly.
func central(args []string) error {
	command := newCentralCommand()
	command.SetArgs(args)
	return command.Execute()
}

func executeCentral(operation string) error {
	switch operation {
	case "deploy":
		if !regexp.MustCompile(`^[0-9a-fA-F]{32}$`).MatchString(os.Getenv("CLOUDFLARE_ACCOUNT_ID")) {
			return errors.New("CLOUDFLARE_ACCOUNT_ID must be a 32-character hexadecimal account ID")
		}
		project, branch := os.Getenv("PAGES_PROJECT"), os.Getenv("PAGES_BRANCH")
		if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(project) {
			return errors.New("PAGES_PROJECT must be a Pages project name (lowercase letters, digits and hyphens), not a domain or URL")
		}
		if strings.TrimSpace(branch) == "" {
			return errors.New("PAGES_BRANCH is required")
		}
		if strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN")) == "" {
			return errors.New("CLOUDFLARE_API_TOKEN is required")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "wrangler", "pages", "deploy", ".build/public", "--project-name", project, "--branch", branch)
		cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
		return cmd.Run()
	case "sign", "apt-smoke", "smoke":
	default:
		return errors.New("unknown central operation")
	}
	data, err := safe.Regular(".build/publication.json")
	if err != nil {
		return err
	}
	var publication struct {
		Epoch   int64  `json:"epoch"`
		Primary string `json:"primary_fingerprint"`
		Signing string `json:"signing_fingerprint"`
	}
	if err = json.Unmarshal(data, &publication); err != nil {
		return err
	}
	if publication.Epoch <= 0 || publication.Primary == "" || publication.Signing == "" {
		return errors.New("invalid publication context")
	}
	opts := defaultRepositoryOptions()
	opts.epoch = publication.Epoch
	opts.primaryFingerprint = publication.Primary
	opts.signingFingerprint = publication.Signing
	return executeRepository(operation, opts)
}

func newUpdatePublicationCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update-publication",
		Short: "Collect and persist the current publication inputs",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return executeUpdatePublication()
		},
	}
}

// updatePublication is retained as a compatibility wrapper for callers that
// used the old entry point directly.
func updatePublication(args []string) error {
	command := newUpdatePublicationCommand()
	command.SetArgs(args)
	return command.Execute()
}

func executeUpdatePublication() error {
	data, err := safe.Regular("publication/context.json")
	if err != nil {
		return err
	}
	var publication map[string]json.RawMessage
	if err = json.Unmarshal(data, &publication); err != nil {
		return err
	}
	for _, key := range []string{"primary_fingerprint", "signing_fingerprint"} {
		var fingerprint string
		if err = json.Unmarshal(publication[key], &fingerprint); err != nil {
			return err
		}
		if !regexp.MustCompile(`^[0-9A-F]{40}$`).MatchString(fingerprint) {
			return errors.New("invalid publication fingerprint")
		}
	}
	work, err := os.MkdirTemp("", "moraxyc-publication-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	snap := filepath.Join(work, "snapshot.json")
	if err = run([]string{"collect", "--snapshot", snap}); err != nil {
		return err
	}
	if err = run([]string{"validate-snapshot", "--snapshot", snap}); err != nil {
		return err
	}
	epoch := time.Now().Unix()
	previous, err := snapshot.Load("publication/snapshot.json")
	if err != nil {
		return err
	}
	collected, err := snapshot.Load(snap)
	if err != nil {
		return err
	}
	cfg, err := config.LoadRepositoryConfig("config/repository.toml")
	if err != nil {
		return err
	}
	policies, err := projects.DiscoverProjects("projects", cfg)
	if err != nil {
		return err
	}
	records, err := collected.Records(cfg, policies)
	if err != nil {
		return err
	}
	collected.IndexHistory, err = apt.RetainIndexes(cfg, records, previous.IndexHistory, epoch)
	if err != nil {
		return err
	}
	if err = collected.Save(snap); err != nil {
		return err
	}
	publication["epoch"] = json.RawMessage(fmt.Sprint(epoch))
	data, err = json.MarshalIndent(publication, "", "  ")
	if err != nil {
		return err
	}
	snapshotData, err := safe.Regular(snap)
	if err != nil {
		return err
	}
	if err = safe.Atomic("publication/snapshot.json", snapshotData); err != nil {
		return err
	}
	return safe.Atomic("publication/context.json", append(data, '\n'))
}
