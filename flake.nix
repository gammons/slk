{
  description = "Nix flake for slk";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f system);
    in
    {
      packages = forAllSystems
        (system:
          let
            pkgs = import nixpkgs { inherit system; };
            version = "unstable-${self.shortRev or "dirty"}";
          in
          {
            default = pkgs.buildGoModule {
              pname = "slk";
              inherit version;
              src = self;
              subPackages = [ "cmd/slk" ];
              vendorHash = "sha256-TxM62vqlePJwOTgTY5cLD6Pt9y6fOWYagPEnhVMSEz8=";
              nativeBuildInputs = with pkgs; [ pkg-config ];
              buildInputs = with pkgs;
                pkgs.lib.optionals pkgs.stdenv.isLinux [
                  wayland
                  libx11
                ];

              ldflags = [
                "-s"
                "-w"
                "-X main.version=${version}"
                "-X main.commit=${self.shortRev or "dirty"}"
                "-X main.date=unknown"
              ];

              meta = with pkgs.lib; {
                description = "A blazingly fast Slack TUI";
                homepage = "https://github.com/gammons/slk";
                license = licenses.mit;
                mainProgram = "slk";
                platforms = platforms.linux ++ platforms.darwin;
              };
            };
          });

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/slk";
        };
      });

      devShells = forAllSystems
        (system:
          let
            pkgs = import nixpkgs { inherit system; };
          in
          {
            default = pkgs.mkShell {
              packages = with pkgs; [ go gopls ];
            };
          });
    };
}
