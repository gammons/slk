{
  description = "A blazingly fast Slack TUI";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };
        lib = pkgs.lib;
        slk = pkgs.buildGo126Module {
          pname = "slk";
          version = "0.0.0";
          src = ./.;
          vendorHash = "sha256-VPmvgB8BkjI3DOdl4XMqT0nxXKdz/v0Hs06veKtfvoY=";
          buildInputs = [pkgs.libX11];
          # The test suite spins up httptest servers on loopback. The macOS
          # Nix sandbox denies all networking by default, so allow loopback
          # there; the attribute is a no-op on Linux.
          __darwinAllowLocalNetworking = true;
        };
        # At this lock, nixpkgs builds libwebsockets with its event-loop
        # plugin directory as the store path twice over, so ttyd can't load
        # evlib_uv, exits at startup, and VHS fails with
        # ERR_CONNECTION_REFUSED. This is nixpkgs' own fix ("libwebsockets:
        # fix plugin search path", 3c38ce6492); drop it once the nixpkgs
        # lock is past that commit.
        libwebsockets-fixed = pkgs.libwebsockets.overrideAttrs (old: {
          postPatch = old.postPatch + ''
            substituteInPlace cmake/lws_config.h.in \
              --replace-fail '"''${CMAKE_INSTALL_PREFIX}/''${LWS_INSTALL_LIB_DIR}"' '"''${CMAKE_INSTALL_FULL_LIBDIR}"'
          '';
        });
        ttyd-fixed = pkgs.ttyd.override { libwebsockets = libwebsockets-fixed; };
        # VHS with sixel capture (charmbracelet/vhs#783), so the demo GIFs
        # show real images instead of half-blocks. Switch back to pkgs.vhs
        # once that PR is in a release. Go 1.26 because the fork needs Go
        # 1.25.12+, newer than this nixpkgs' go_1_25. Its tests need ttyd
        # and a browser, which the build sandbox lacks.
        vhs-sixel = (pkgs.vhs.override {
          buildGoModule = pkgs.buildGo126Module;
          ttyd = ttyd-fixed;
        }).overrideAttrs (old: {
          version = "${old.version}-sixel";
          src = pkgs.fetchFromGitHub {
            owner = "jchook";
            repo = "vhs";
            rev = "113917ffe5585f19b58596f01b141eb1ebc01b54";
            hash = "sha256-lL5+VOEGP3yiDhUO7CH2CI2Jvy09Zt12YJTiQJesSJ4=";
          };
          vendorHash = "sha256-0/GA+AGyHiw7PvUXRBuL9yylE3NOY3vVUA3uSFAP11Q=";
          doCheck = false;
        });
      in {
        packages.default = slk;
        packages.slk = slk;
        # `nix develop`: slk's build inputs (Go, libX11) plus vhs, for `make demo-gifs`.
        devShells.default = pkgs.mkShell {
          inputsFrom = [ slk ];
          packages = [ vhs-sixel ];
        };
      });
}
