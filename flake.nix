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
          vendorHash = "sha256-/J4gr4m9v6Y0Be8BU4wepIdl2sjoPh0pFCvJL2kIeLk=";
          buildInputs = [pkgs.libX11];
        };
      in {
        packages.default = slk;
        packages.slk = slk;
      });
}
