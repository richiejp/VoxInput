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
        in
        {
          default = pkgs.buildGoModule {
            pname = "voxinput";
            version = "0.1.0";

            # Path to the source code
            src = ./.;

            vendorHash = "sha256-OserWlRhKyTvLrYSikNCjdDdTATIcWTfqJi9n4mHVLE="; #nixpkgs.lib.fakeHash;

            # Include runtime dependencies
            buildInputs = with pkgs; [
              libpulseaudio
              dotool
            ];

            # Ensure libpulseaudio is available at runtime
            LD_LIBRARY_PATH = "${pkgs.libpulseaudio}/lib";

            # To take advantage of this something like services.udev.packages = [ nixpkgs.voxinput ] is required
            postInstall = ''
              mv $out/bin/VoxInput $out/bin/voxinput
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
