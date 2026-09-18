{ inputs, ... }:
{
  perSystem =
    {
      config,
      pkgs,
      system,
      ...
    }:
    let
      go2nix = inputs.go2nix.packages.${system};
      goEnv = inputs.go2nix.lib.mkGoEnv {
        inherit (pkgs) go callPackage;
        inherit (go2nix) go2nix;
      };
    in
    {
      packages = {
        default = config.packages.moraxyc-repo;
        publication-tools = pkgs.buildEnv {
          name = "moraxyc-publication-tools";
          paths = with pkgs; [
            config.packages.moraxyc-repo
            bash
            coreutils
            diffutils
            git
            gh
            nixVersions.latest
            jq
            apt
            dpkg
            gnupg
            wrangler
          ];
          pathsToLink = [ "/bin" ];
        };
        moraxyc-repo = goEnv.buildGoApplication {
          pname = "moraxyc-repo";
          version = "0.1.0";
          src = ../.;
          goLock = ../go2nix.toml;
          subPackages = [ "cmd/moraxyc-repo" ];
          # The standalone test package is not reachable from the CLI imports.
          extraMainSrcFiles = [ "tests" ];
          nativeBuildInputs = with pkgs; [
            apt
            dpkg
            gnupg
            gzip
          ];
          meta.mainProgram = "moraxyc-repo";
        };
        inherit (go2nix) go2nix;
        publication-nix = pkgs.writeShellScriptBin "nix" ''
          exec ${config.packages.nix-go2nix}/bin/nix-go2nix "$@"
        '';
        nix-go2nix = pkgs.writeShellScriptBin "nix-go2nix" ''
          exec ${pkgs.nixVersions.latest}/bin/nix \
            --option plugin-files ${go2nix.go2nix-nix-plugin}/lib/nix/plugins/libgo2nix_plugin.so "$@"
        '';
      };
    };
}
