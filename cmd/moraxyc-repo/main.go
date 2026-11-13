package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"pkgs.moraxyc.com/internal/apt"
	"pkgs.moraxyc.com/internal/artifact"
	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/github"
	"pkgs.moraxyc.com/internal/projects"
	"pkgs.moraxyc.com/internal/safe"
	"pkgs.moraxyc.com/internal/signing"
	"pkgs.moraxyc.com/internal/snapshot"
	"pkgs.moraxyc.com/internal/verify"
)

type repositoryOptions struct {
	root               string
	snapshot           string
	out                string
	keyring            string
	primaryFingerprint string
	signingFingerprint string
	epoch              int64
	signed             bool
}

func defaultRepositoryOptions() repositoryOptions {
	return repositoryOptions{
		root:     ".",
		snapshot: ".build/snapshot.json",
		out:      ".build/public",
		keyring:  "keys/moraxyc-archive-keyring.gpg",
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := newRootCommand()
	command.SetArgs(args)
	return command.Execute()
}

func newRootCommand() *cobra.Command {
	command := &cobra.Command{
		Use:           "moraxyc-repo",
		Short:         "Build and verify the Moraxyc APT repository",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	command.AddCommand(
		newRepositoryLeafCommand("check-config", "Validate repository configuration and project policies", func(cmd *cobra.Command, opts *repositoryOptions) {
			cmd.Flags().StringVar(&opts.root, "root", opts.root, "repository root")
		}, func(opts repositoryOptions) error {
			return executeRepository("check-config", opts)
		}),
		newRepositoryLeafCommand("collect", "Collect release metadata and manifests", func(cmd *cobra.Command, opts *repositoryOptions) {
			addConfigFlags(cmd, opts)
			cmd.Flags().StringVar(&opts.snapshot, "snapshot", opts.snapshot, "snapshot file")
		}, func(opts repositoryOptions) error {
			return executeRepository("collect", opts)
		}),
		newRepositoryLeafCommand("validate-snapshot", "Validate a collected snapshot", func(cmd *cobra.Command, opts *repositoryOptions) {
			addConfigFlags(cmd, opts)
			cmd.Flags().StringVar(&opts.snapshot, "snapshot", opts.snapshot, "snapshot file")
		}, func(opts repositoryOptions) error {
			return executeRepository("validate-snapshot", opts)
		}),
		newRepositoryLeafCommand("render", "Render an unsigned APT repository", func(cmd *cobra.Command, opts *repositoryOptions) {
			addPublicationFlags(cmd, opts)
		}, func(opts repositoryOptions) error {
			return executeRepository("render", opts)
		}),
		newRepositoryLeafCommand("verify", "Verify the rendered repository", func(cmd *cobra.Command, opts *repositoryOptions) {
			addPublicationFlags(cmd, opts)
			cmd.Flags().BoolVar(&opts.signed, "signed", opts.signed, "require both signatures")
		}, func(opts repositoryOptions) error {
			return executeRepository("verify", opts)
		}),
		newRepositoryLeafCommand("sign", "Sign and verify the rendered repository", func(cmd *cobra.Command, opts *repositoryOptions) {
			addPublicationFlags(cmd, opts)
		}, func(opts repositoryOptions) error {
			return executeRepository("sign", opts)
		}),
		newRepositoryLeafCommand("apt-smoke", "Run an isolated APT smoke test", func(cmd *cobra.Command, opts *repositoryOptions) {
			addPublicationFlags(cmd, opts)
		}, func(opts repositoryOptions) error {
			return executeRepository("apt-smoke", opts)
		}),
		newRepositoryLeafCommand("smoke", "Run local and remote smoke tests", func(cmd *cobra.Command, opts *repositoryOptions) {
			addPublicationFlags(cmd, opts)
		}, func(opts repositoryOptions) error {
			return executeRepository("smoke", opts)
		}),
		newArtifactLeafCommand("manifest", "Generate and verify a project artifact manifest", "Generate a manifest from project artifacts", func(opts artifactOptions) error {
			return executeArtifact("manifest", opts)
		}),
		newArtifactLeafCommand("verify-artifacts", "Verify project artifacts", "Verify project artifact names, hashes and Debian fields", func(opts artifactOptions) error {
			return executeArtifact("verify-artifacts", opts)
		}),
		newProjectCICommand(),
		newCentralCommand(),
		newUpdatePublicationCommand(),
	)

	return command
}

func addConfigFlags(command *cobra.Command, opts *repositoryOptions) {
	command.Flags().StringVar(&opts.root, "root", opts.root, "repository root")
}

func addPublicationFlags(command *cobra.Command, opts *repositoryOptions) {
	addConfigFlags(command, opts)
	command.Flags().StringVar(&opts.snapshot, "snapshot", opts.snapshot, "snapshot file")
	command.Flags().StringVar(&opts.out, "out", opts.out, "new public directory")
	command.Flags().StringVar(&opts.keyring, "keyring", opts.keyring, "public keyring")
	command.Flags().StringVar(&opts.primaryFingerprint, "primary-fingerprint", opts.primaryFingerprint, "primary public fingerprint")
	command.Flags().StringVar(&opts.signingFingerprint, "signing-fingerprint", opts.signingFingerprint, "signing subkey fingerprint")
	command.Flags().Int64Var(&opts.epoch, "epoch", opts.epoch, "explicit UTC epoch (or SOURCE_DATE_EPOCH)")
}

func newRepositoryLeafCommand(use, short string, addFlags func(*cobra.Command, *repositoryOptions), execute func(repositoryOptions) error) *cobra.Command {
	opts := defaultRepositoryOptions()
	command := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return execute(opts)
		},
	}
	addFlags(command, &opts)
	return command
}

type artifactOptions struct {
	directory     string
	project       string
	tag           string
	architectures string
}

func newArtifactLeafCommand(use, short, long string, execute func(artifactOptions) error) *cobra.Command {
	opts := artifactOptions{directory: "dist", architectures: "amd64,arm64"}
	command := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return execute(opts)
		},
	}
	command.Flags().StringVar(&opts.directory, "artifacts", opts.directory, "project artifact directory")
	command.Flags().StringVar(&opts.project, "project", opts.project, "project ID")
	command.Flags().StringVar(&opts.tag, "tag", opts.tag, "release tag")
	command.Flags().StringVar(&opts.architectures, "architectures", opts.architectures, "accepted project architectures")
	return command
}

func executeArtifact(command string, opts artifactOptions) error {
	if command == "manifest" {
		manifest, err := artifact.Generate(opts.directory, opts.project, opts.tag)
		if err != nil {
			return err
		}
		data, err := manifest.Bytes()
		if err != nil {
			return err
		}
		if err = safe.Atomic(filepath.Join(opts.directory, "moraxyc-manifest.json"), data); err != nil {
			return err
		}
	}
	return artifact.Verify(opts.directory, opts.project, opts.tag, strings.Split(opts.architectures, ","))
}

func signingSubkey() ([]byte, error) {
	encoded := strings.TrimSpace(os.Getenv("APT_SIGNING_SUBKEY"))
	if encoded == "" {
		return nil, errors.New("APT_SIGNING_SUBKEY is missing")
	}
	secret, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("APT_SIGNING_SUBKEY must be a base64-encoded secret subkey export")
	}
	return secret, nil
}

func executeRepository(command string, opts repositoryOptions) error {
	configData, err := config.LoadRepositoryConfig(filepath.Join(opts.root, "config/repository.toml"))
	if err != nil {
		return err
	}
	projectPolicies, err := projects.DiscoverProjects(filepath.Join(opts.root, "projects"), configData)
	if err != nil {
		return err
	}
	if command == "check-config" {
		fmt.Printf("validated %d project policies\n", len(projectPolicies))
		return nil
	}
	if command == "collect" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		collected, err := github.New(os.Getenv("GITHUB_TOKEN")).Collect(ctx, configData, projectPolicies, time.Now())
		if err != nil {
			return err
		}
		return collected.Save(opts.snapshot)
	}

	collected, err := snapshot.Load(opts.snapshot)
	if err != nil {
		return err
	}
	records, err := collected.Records(configData, projectPolicies)
	if err != nil {
		return err
	}
	if command == "validate-snapshot" {
		if _, err = apt.PublicationIndexes(configData, records, collected.IndexHistory, 0); err != nil {
			return err
		}
		missing := 0
		for _, project := range collected.Projects {
			for _, release := range project.Releases {
				for _, asset := range release.Assets {
					if strings.HasSuffix(asset.Name, ".deb") && asset.Digest == "" {
						missing++
					}
				}
			}
		}
		fmt.Printf("validated %d projects, %d packages; %d API asset digests unverified\n", len(collected.Projects), len(records), missing)
		return nil
	}
	if opts.epoch == 0 {
		opts.epoch, err = strconv.ParseInt(os.Getenv("SOURCE_DATE_EPOCH"), 10, 64)
		if err != nil {
			return errors.New("explicit epoch required")
		}
	}
	key, err := safe.Regular(opts.keyring)
	if err != nil {
		return err
	}
	keyEpoch := int64(0)
	if command == "render" {
		keyEpoch = opts.epoch
	}
	if err = signing.PublicAt(key, opts.primaryFingerprint, opts.signingFingerprint, keyEpoch); err != nil {
		return err
	}
	switch command {
	case "render":
		if err = apt.Render(configData, records, key, opts.epoch, opts.out, collected.IndexHistory); err != nil {
			return err
		}
		return verify.Tree(opts.out, configData, records, key, opts.epoch, opts.primaryFingerprint, opts.signingFingerprint, false, collected.IndexHistory)
	case "verify":
		return verify.Tree(opts.out, configData, records, key, opts.epoch, opts.primaryFingerprint, opts.signingFingerprint, opts.signed, collected.IndexHistory)
	case "apt-smoke":
		if err = verify.Tree(opts.out, configData, records, key, opts.epoch, opts.primaryFingerprint, opts.signingFingerprint, true, collected.IndexHistory); err != nil {
			return err
		}
		return verify.APT(opts.out, configData, records)
	case "smoke":
		if err = verify.Tree(opts.out, configData, records, key, opts.epoch, opts.primaryFingerprint, opts.signingFingerprint, true, collected.IndexHistory); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
		defer cancel()
		return verify.Remote(ctx, strings.TrimSuffix(configData.Repository.BaseURL, "/deb"), opts.out, configData, records, collected.IndexHistory, opts.epoch)
	case "sign":
		if err = verify.Tree(opts.out, configData, records, key, opts.epoch, opts.primaryFingerprint, opts.signingFingerprint, false, collected.IndexHistory); err != nil {
			return err
		}
		secret, err := signingSubkey()
		if err != nil {
			return err
		}
		if err = signing.Sign(opts.out, key, secret, []byte(os.Getenv("APT_SIGNING_PASSPHRASE")), opts.primaryFingerprint, opts.signingFingerprint); err != nil {
			return err
		}
		return verify.Tree(opts.out, configData, records, key, opts.epoch, opts.primaryFingerprint, opts.signingFingerprint, true, collected.IndexHistory)
	default:
		return errors.New("unknown command")
	}
}
