package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCentralDeploy(t *testing.T) {
	account := strings.Repeat("a1", 16)
	for _, test := range []struct {
		name, key, value, message string
		exitCode                  string
	}{
		{name: "deploy", exitCode: "0"},
		{name: "wrangler failure", exitCode: "7"},
		{name: "missing account", key: "CLOUDFLARE_ACCOUNT_ID", message: "CLOUDFLARE_ACCOUNT_ID"},
		{name: "account URL", key: "CLOUDFLARE_ACCOUNT_ID", value: "https://dash.cloudflare.com/" + account, message: "CLOUDFLARE_ACCOUNT_ID"},
		{name: "account newline", key: "CLOUDFLARE_ACCOUNT_ID", value: account + "\n", message: "CLOUDFLARE_ACCOUNT_ID"},
		{name: "nonhex account", key: "CLOUDFLARE_ACCOUNT_ID", value: strings.Repeat("z", 32), message: "CLOUDFLARE_ACCOUNT_ID"},
		{name: "missing project", key: "PAGES_PROJECT", message: "PAGES_PROJECT"},
		{name: "project domain", key: "PAGES_PROJECT", value: "pkgs.moraxyc.com", message: "PAGES_PROJECT"},
		{name: "project URL", key: "PAGES_PROJECT", value: "https://pkgs.pages.dev", message: "PAGES_PROJECT"},
		{name: "project newline", key: "PAGES_PROJECT", value: "pkgs\n", message: "PAGES_PROJECT"},
		{name: "missing branch", key: "PAGES_BRANCH", message: "PAGES_BRANCH"},
		{name: "blank branch", key: "PAGES_BRANCH", value: " \n", message: "PAGES_BRANCH"},
		{name: "missing token", key: "CLOUDFLARE_API_TOKEN", message: "CLOUDFLARE_API_TOKEN"},
		{name: "blank token", key: "CLOUDFLARE_API_TOKEN", value: " \n", message: "CLOUDFLARE_API_TOKEN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			output := filepath.Join(dir, "args")
			script := "#!/bin/sh\nprintf '%s\\n' \"$CLOUDFLARE_ACCOUNT_ID\" \"$@\" > \"$DEPLOY_ARGS\"\nexit \"$DEPLOY_EXIT_CODE\"\n"
			if err := os.WriteFile(filepath.Join(dir, "wrangler"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			t.Setenv("DEPLOY_ARGS", output)
			t.Setenv("DEPLOY_EXIT_CODE", test.exitCode)
			t.Setenv("CLOUDFLARE_ACCOUNT_ID", account)
			t.Setenv("PAGES_PROJECT", "pkgs-moraxyc")
			t.Setenv("PAGES_BRANCH", "main")
			t.Setenv("CLOUDFLARE_API_TOKEN", "synthetic-token")
			if test.key != "" {
				t.Setenv(test.key, test.value)
			}

			err := executeCentral("deploy")
			if test.message != "" {
				if err == nil || !strings.Contains(err.Error(), test.message) {
					t.Fatalf("expected configuration error mentioning %s, got %v", test.message, err)
				}
				if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("invalid configuration invoked wrangler: %v", statErr)
				}
				return
			}
			if test.exitCode == "0" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
					t.Fatalf("wrangler failure was not propagated: %v", err)
				}
			}
			args, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			want := account + "\npages\ndeploy\n.build/public\n--project-name\npkgs-moraxyc\n--branch\nmain\n"
			if string(args) != want {
				t.Fatalf("unexpected wrangler invocation: %q", args)
			}
		})
	}
}
