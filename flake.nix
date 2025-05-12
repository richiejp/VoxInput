{
  description = "VoxInput";

  inputs.nixpkgs.url = "nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      supportedSystems = [ "x86_64-linux" "aarch64-linux" ];
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

      packages = forAllSystems (system:
        let
          pkgs = nixpkgsFor.${system};
          lib = pkgs.lib;
        in
        {
          default = pkgs.buildGoModule {
            pname = "voxinput";
            version = "0.3.0";

            # Path to the source code
            src = ./.;

            vendorHash = "sha256-OserWlRhKyTvLrYSikNCjdDdTATIcWTfqJi9n4mHVLE="; #nixpkgs.lib.fakeHash;

            nativeBuildInputs = with pkgs; [
              makeWrapper
            ];

            # Include runtime dependencies
            buildInputs = with pkgs; [
              libpulseaudio
              dotool
            ];

            postInstall = with pkgs; ''
              mv $out/bin/VoxInput $out/bin/voxinput
              wrapProgram $out/bin/voxinput \
                --prefix PATH : ${lib.makeBinPath [ dotool ]} \
                --prefix LD_LIBRARY_PATH : "${lib.makeLibraryPath [ libpulseaudio ]}"
              mkdir -p $out/lib/udev/rules.d
              echo 'KERNEL=="uinput", GROUP="input", MODE="0620", OPTIONS+="static_node=uinput"' > $out/lib/udev/rules.d/99-voxinput.rules
            '';

            meta = with pkgs.lib; {
              description = "Transcribe input from your microphone and turn it into key presses on a virtual keyboard.";
              license = licenses.mit;
              maintainers = [ maintainers.richiejp ];
              platforms = platforms.unix;
            };
          };
        });
    };
}
