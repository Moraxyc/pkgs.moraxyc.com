{
  imports = [
    ./checks.nix
    ./shell.nix
  ];
  perSystem = { pkgs, ... }: { formatter = pkgs.nixfmt; };
}
