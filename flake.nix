{
  description = "VoxInput";

  inputs.nixpkgs.url = "nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      supportedSystems = [ "x86_64-linux" "x86_64-darwin" "aarch64-linux" "aarch64-darwin" ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
      nixpkgsFor = forAllSystems (system: import nixpkgs { inherit system; });
    in
    {
      devShells = forAllSystems (system:
        let
          pkgs = nixpkgsFor.${system};
        in
        {
          default = pkgs.mkShell.override { stdenv = pkgs.clangStdenv; } {
            buildInputs = with pkgs; [
              go
              gopls
              gotools
              go-tools

              libpulseaudio
              dotool
            ];
            LD_LIBRARY_PATH = "${pkgs.libpulseaudio}/lib";
          };
        });
    };
}
