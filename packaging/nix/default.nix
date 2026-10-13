{ pkgs ? import <nixpkgs> {}, program ? "goteddycloud", source ? null }:
let
  release = builtins.fromJSON (builtins.readFile ./release.json);
  assets = builtins.fromJSON (builtins.readFile ./assets.json);
  system = pkgs.stdenv.hostPlatform.system;
  asset = assets.${program}.${system} or
    (throw "No ${program} release binary for ${system}");
in pkgs.stdenvNoCC.mkDerivation {
  pname = program;
  version = pkgs.lib.removePrefix "v" release.version;
  src = if source != null then source else pkgs.fetchurl {
    url = "https://github.com/${release.repository}/releases/download/${release.version}/${asset.file}";
    sha256 = asset.sha256;
  };
  dontBuild = true;
  dontStrip = true;
  dontFixup = true; # Preserve already-built static Go executables and Mach-O signatures.
  installPhase = ''
    runHook preInstall
    install -Dm755 ${program} "$out/bin/${program}"
    mkdir -p "$out/share/doc/${program}"
    for file in *; do
      if test -f "$file" && test "$file" != "${program}"; then
        cp "$file" "$out/share/doc/${program}/"
      fi
    done
    runHook postInstall
  '';
  meta = {
    description = if program == "goteddycloud" then "Go TeddyCloud server" else "Native Go CC3200 tool";
    homepage = "https://github.com/${release.repository}";
    license = pkgs.lib.licenses.gpl2Only;
    mainProgram = program;
    platforms = builtins.attrNames assets.${program};
  };
}
