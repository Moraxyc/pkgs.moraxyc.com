{
  perSystem =
    { config, pkgs, ... }:
    {
      devShells.publication = pkgs.mkShell {
        packages = [ config.packages.publication-tools ];
        PUBLICATION_TOOLS = config.packages.publication-tools;
      };
      devShells.default = pkgs.mkShell {
        packages = with pkgs; [
          go
          gh
          curl
          jq
          apt
          dpkg
          gnupg
          gzip
          just
          wrangler
          actionlint
          nixfmt
        ];
        shellHook = ''
          export GOMODCACHE="$PWD/.build/go-modules"
          export GOCACHE="$PWD/.build/go-cache"
        '';
      };
    };
}
