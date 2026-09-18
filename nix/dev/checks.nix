{
  perSystem =
    { config, pkgs, ... }:
    {
      checks = {
        package = config.packages.moraxyc-repo;
        lint =
          pkgs.runCommand "moraxyc-repo-lint"
            {
              nativeBuildInputs = with pkgs; [
                go
                actionlint
                nixfmt
                config.packages.go2nix
              ];
            }
            ''
              cd ${../..}
              go2nix check
              test -z "$(gofmt -l cmd internal schemas tests)"
              nixfmt --check flake.nix nix/*.nix nix/dev/*.nix
              actionlint .github/workflows/*.yml examples/project/.github/workflows/*.yml
              ${config.packages.moraxyc-repo}/bin/moraxyc-repo check-config
              touch "$out"
            '';
      };
    };
}
