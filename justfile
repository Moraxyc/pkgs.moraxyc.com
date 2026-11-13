set shell := ["bash", "-euo", "pipefail", "-c"]

check:
    nixfmt --check flake.nix nix/*.nix nix/dev/*.nix
    test -z "$(gofmt -l cmd internal schemas tests)"
    actionlint .github/workflows/*.yml examples/project/.github/workflows/*.yml
    go vet ./...
    go test ./...
    go run ./cmd/moraxyc-repo check-config

build:
    mkdir -p .build
    nix build .#moraxyc-repo --out-link .build/package
    ln -sfn package/bin/moraxyc-repo .build/moraxyc-repo

update:
    nix flake update
    nix develop -c just vendor-hash

vendor-hash:
    #!/usr/bin/env bash
    set -euo pipefail
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    GOTOOLCHAIN=local go mod vendor -o "$tmp/vendor"
    hash=$(nix hash path "$tmp/vendor")
    printf '%s\n' "$hash" > nix/vendor-hash

site:
    nix build .#site --out-link .build/site

integration:
    MORAXYC_INTEGRATION=1 go test ./tests -count=1 -v

preview:
    MORAXYC_INTEGRATION=1 MORAXYC_PREVIEW_DIR="$PWD/.build/preview" go test ./tests -run '^TestSignedRepositoryAndTampering$' -count=1
