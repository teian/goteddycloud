# Nix release packages

Extract this archive, then build either package with your configured nixpkgs:

```sh
nix-build default.nix
nix-build default.nix --argstr program cc3200tool
```

Or install with `nix-env -if default.nix`; add `--argstr program cc3200tool`
to select the standalone serial tool. This is a Nix package expression for the
published native release binaries, not an upstream nixpkgs submission or a binary
cache. `assets.json` pins each download's SHA-256. Systems without a release
binary fail explicitly. No compiler or CC3200 loader DLL is downloaded.

Frontend assets remain separately supplied. Before starting a new deployment,
set `core.wwwdir` to your own frontend asset directory using `-config-set`.
Keep the deployment's writable data outside the Nix store. FFmpeg remains an
optional external runtime dependency for encoding; existing TAF delivery needs no
FFmpeg. Configure management login and HTTPS before exposing the Web UI.

For offline validation against an already-downloaded matching release archive,
pass `--arg source /absolute/path/to/program-release.tar.gz` to `nix-build`.
