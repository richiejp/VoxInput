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
          deepvqe-lib = pkgs.stdenv.mkDerivation {
            pname = "libdeepvqe";
            version = "0.1.0";
            src = ./deepvqe-ggml/ggml;
            nativeBuildInputs = [ pkgs.cmake ];
            cmakeFlags = [ "-DDEEPVQE_BUILD_SHARED=ON" "-DCMAKE_BUILD_TYPE=Release" ];
            installPhase = ''
              mkdir -p $out/lib $out/include
              cp libdeepvqe.so* $out/lib/ || true
              cp libdeepvqe.so $out/lib/ || true
              cp ${./deepvqe-ggml/ggml/deepvqe_api.h} $out/include/
            '';
          };
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
              deepvqe-lib

              libGL pkg-config xorg.libX11.dev xorg.libXcursor xorg.libXi xorg.libXinerama xorg.libXrandr xorg.libXxf86vm libxkbcommon wayland
            ];
            LD_LIBRARY_PATH = pkgs.lib.makeLibraryPath [ pkgs.libpulseaudio deepvqe-lib ];
          };
        });

      packages = forAllSystems (system:
        let
          pkgs = nixpkgsFor.${system};
          lib = pkgs.lib;
          stdenv = pkgs.stdenv;
          deepvqe-lib = pkgs.stdenv.mkDerivation {
            pname = "libdeepvqe";
            version = "0.1.0";
            src = ./deepvqe-ggml/ggml;
            nativeBuildInputs = [ pkgs.cmake ];
            cmakeFlags = [ "-DDEEPVQE_BUILD_SHARED=ON" "-DCMAKE_BUILD_TYPE=Release" ];
            installPhase = ''
              mkdir -p $out/lib $out/include
              cp libdeepvqe.so* $out/lib/ || true
              cp libdeepvqe.so $out/lib/ || true
              cp ${./deepvqe-ggml/ggml/deepvqe_api.h} $out/include/
            '';
          };
        in
        {
          default = pkgs.buildGoModule {
            pname = "voxinput";
            # sync with main.go
            version = lib.strings.removeSuffix "\n" (builtins.readFile ./version.txt);

            # Path to the source code
            src = ./.;

            vendorHash = null; # will need updating after go mod tidy

            nativeBuildInputs = with pkgs; [
              makeWrapper
              pkg-config
            ];

            # Include runtime dependencies
            buildInputs = with pkgs; [
              libpulseaudio
              dotool
              deepvqe-lib

              libGL xorg.libX11.dev xorg.libXcursor xorg.libXi xorg.libXinerama xorg.libXrandr xorg.libXxf86vm libxkbcommon wayland
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
                --add-rpath ${lib.makeLibraryPath [ pkgs.libpulseaudio deepvqe-lib ]}
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
