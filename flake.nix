{
  description = "VoxInput";

  inputs.nixpkgs.url = "nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      supportedSystems = [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ];
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

              libGL pkg-config xorg.libX11.dev xorg.libXcursor xorg.libXi xorg.libXinerama xorg.libXrandr xorg.libXxf86vm libxkbcommon wayland
            ];
            LD_LIBRARY_PATH = "${pkgs.libpulseaudio}/lib";
          };
        });

      packages = forAllSystems (system:
        let
          pkgs = nixpkgsFor.${system};
          lib = pkgs.lib;
          stdenv = pkgs.stdenv;
        in
        {
          default = pkgs.buildGoModule {
            pname = "voxinput";
            version = "0.4.0";

            # Path to the source code
            src = ./.;

            vendorHash = "sha256-ZMnRHvP4zaJq1BWMC9aR1+e5QMjqTaPV+jL4bv8lMMQ="; #nixpkgs.lib.fakeHash;

            nativeBuildInputs = with pkgs; [
              makeWrapper
            ];

            # Include runtime dependencies
            buildInputs = with pkgs; [
              libpulseaudio
              dotool
            ];

            postInstall = ''
              mv $out/bin/VoxInput $out/bin/voxinput_tmp ; mv $out/bin/voxinput_tmp $out/bin/voxinput
            ''
            + lib.optionalString stdenv.hostPlatform.isLinux ''
              wrapProgram $out/bin/voxinput \
                --prefix PATH : ${lib.makeBinPath [ pkgs.dotool ]}
              mkdir -p $out/lib/udev/rules.d
              echo 'KERNEL=="uinput", GROUP="input", MODE="0620", OPTIONS+="static_node=uinput"' > $out/lib/udev/rules.d/99-voxinput.rules
            '';

            postFixup = lib.optionalString stdenv.hostPlatform.isElf ''
              patchelf $out/bin/.voxinput-wrapped \
                --add-rpath ${lib.makeLibraryPath [ pkgs.libpulseaudio ]}
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
