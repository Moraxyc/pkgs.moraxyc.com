{ ... }:
{
  perSystem =
    { config, pkgs, ... }:
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
        moraxyc-repo = pkgs.buildGoModule {
          pname = "moraxyc-repo";
          version = "0.1.0";
          src = pkgs.lib.fileset.toSource {
            root = ../.;
            fileset = pkgs.lib.fileset.unions [
              ../go.mod
              ../go.sum
              ../LICENSE
              ../cmd
              ../internal
              ../schemas
              ../tests
            ];
          };
          vendorHash = pkgs.lib.fileContents ./vendor-hash;
          goSum = ../go.sum;
          subPackages = [ "cmd/moraxyc-repo" ];
          nativeBuildInputs = with pkgs; [
            apt
            dpkg
            gnupg
            gzip
          ];
          checkPhase = ''
            runHook preCheck
            go test ./...
            runHook postCheck
          '';
          meta.mainProgram = "moraxyc-repo";
        };
      };
    };
}
