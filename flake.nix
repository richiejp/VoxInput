{
  description = "VoxInput";

  inputs = {
    nixpkgs.url = "nixpkgs/nixos-unstable";
    localvqe-src = {
      url = "git+https://github.com/localai-org/LocalVQE?submodules=1";
      flake = false;
    };
  };

  outputs = { self, nixpkgs, localvqe-src }:
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
              cmake

              libGL pkg-config xorg.libX11.dev xorg.libXcursor xorg.libXi xorg.libXinerama xorg.libXrandr xorg.libXxf86vm libxkbcommon wayland
            ];
            LD_LIBRARY_PATH = pkgs.lib.makeLibraryPath [ pkgs.libpulseaudio ];
          };
        });

      packages = forAllSystems (system:
        let
          pkgs = nixpkgsFor.${system};
          lib = pkgs.lib;
          stdenv = pkgs.stdenv;
          localvqe-lib = pkgs.stdenv.mkDerivation {
            pname = "liblocalvqe";
            version = "0.1.0";
            src = localvqe-src + "/ggml";
            nativeBuildInputs = [ pkgs.cmake ];
            cmakeFlags = [ "-DLOCALVQE_BUILD_SHARED=ON" "-DCMAKE_BUILD_TYPE=Release" ];
            installPhase = ''
              mkdir -p $out/lib $out/include
              cp liblocalvqe.so* $out/lib/ || true
              cp liblocalvqe.so $out/lib/ || true
              cp ${localvqe-src + "/ggml/localvqe_api.h"} $out/include/
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

            vendorHash = "sha256-f6eVtsxwsTmdxxgjsWWQXCfAsFO6OUDWUNPraxZfsb4=";

            nativeBuildInputs = with pkgs; [
              makeWrapper
              pkg-config
            ];

            # Include runtime dependencies
            buildInputs = with pkgs; [
              libpulseaudio
              dotool
              localvqe-lib

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
                --add-rpath ${lib.makeLibraryPath [ pkgs.libpulseaudio localvqe-lib ]}
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
