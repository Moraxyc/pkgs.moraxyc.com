{ inputs, ... }:
{
  perSystem =
    { config, pkgs, ... }:
    let
      context = builtins.fromJSON (builtins.readFile ../publication/context.json);
    in
    {
      packages.site =
        pkgs.runCommand "moraxyc-unsigned-site"
          {
            nativeBuildInputs = [
              config.packages.moraxyc-repo
              pkgs.apt
              pkgs.gnupg
            ];
            SOURCE_DATE_EPOCH = toString context.epoch;
            LC_ALL = "C";
            TZ = "UTC";
          }
          ''
            export HOME="$TMPDIR/home"
            mkdir -p "$HOME"
            moraxyc-repo render \
              --root ${inputs.self.outPath} \
              --snapshot ${../publication/snapshot.json} \
              --keyring ${inputs.self.outPath}/keys/moraxyc-archive-keyring.gpg \
              --epoch "$SOURCE_DATE_EPOCH" \
              --primary-fingerprint ${pkgs.lib.escapeShellArg context.primary_fingerprint} \
              --signing-fingerprint ${pkgs.lib.escapeShellArg context.signing_fingerprint} \
              --out "$TMPDIR/public"
            mkdir -p "$out"
            mv "$TMPDIR/public" "$out/public"
            cp ${../publication/snapshot.json} "$out/snapshot.json"
            cp ${../publication/context.json} "$out/publication.json"
          '';
    };
}
