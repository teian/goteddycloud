#!/usr/bin/env python3
"""Build native archives, Linux packages and hash-pinned Nix release packages."""
import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import zipfile

TARGETS = [('linux', 'amd64'), ('linux', 'arm'), ('linux', 'arm64'),
           ('windows', 'amd64'), ('windows', 'arm64'),
           ('darwin', 'amd64'), ('darwin', 'arm64')]
PROGRAMS = ('goteddycloud', 'cc3200tool')
NOTICES = ['LICENSE', 'NOTICE', 'README.md', 'docs/ci.md', 'docs/licenses.md', 'docs/storage.md',
           'docs/storage-verification.md', 'docs/s3-providers.md', 'docs/cc3200-flashing.md',
           'docs/cc3200-tool.md', 'docs/reverse-proxy.md', 'docs/management-tls.md',
           'docs/user-areas.md', 'docs/migration.md', 'docs/verification.md', 'docs/packaging.md',
           'third_party/Go-LICENSE', 'third_party/go-serial-LICENSE',
           'third_party/kong-COPYING', 'third_party/x-term-LICENSE',
           'third_party/x-sys-LICENSE', 'third_party/x-crypto-LICENSE',
           'third_party/x-net-LICENSE', 'third_party/x-text-LICENSE',
           'third_party/libopus-COPYING', 'third_party/gofakes3-LICENSE',
           'third_party/goskiplist-LICENSE']


def architecture(goarch):
    return 'armv7' if goarch == 'arm' else goarch


def archive(directory, destination, windows):
    if windows:
        with zipfile.ZipFile(destination, 'w', compression=zipfile.ZIP_DEFLATED) as output:
            for file in sorted(p for p in directory.rglob('*') if p.is_file()):
                info = zipfile.ZipInfo(f'{directory.name}/{file.relative_to(directory).as_posix()}', date_time=(1980, 1, 1, 0, 0, 0))
                info.external_attr = 0o100644 << 16
                output.writestr(info, file.read_bytes(), compress_type=zipfile.ZIP_DEFLATED)
    else:
        with destination.open('wb') as raw, gzip.GzipFile(filename='', fileobj=raw, mode='wb', mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode='w') as output:
                for file in sorted(p for p in directory.rglob('*') if p.is_file()):
                    data = file.read_bytes()
                    info = tarfile.TarInfo(f'{directory.name}/{file.relative_to(directory).as_posix()}')
                    info.size = len(data)
                    info.mode = 0o755 if file.name in PROGRAMS else 0o644
                    info.mtime = 0
                    output.addfile(info, io.BytesIO(data))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('version', nargs='?', default=Path('VERSION').read_text().strip())
    parser.add_argument('--out', type=Path, default=Path('dist'))
    parser.add_argument('--packages', action='store_true', help='Also build .deb/.rpm/Arch packages using nFPM')
    parser.add_argument('--nfpm', default='nfpm', help='nFPM executable (required with --packages)')
    parser.add_argument('--repository', default='teian/goteddycloud', help='GitHub repository hosting release assets')
    args = parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._+-]{0,79}', args.version):
        parser.error('version must be a safe identifier')
    if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', args.repository):
        parser.error('repository must be owner/name')
    if args.packages and not re.fullmatch(r'v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?', args.version):
        parser.error('Linux packages require a semantic release version, for example v1.2.3')
    if args.packages and shutil.which(args.nfpm) is None:
        parser.error('nFPM executable not found')
    args.out = args.out.resolve()
    args.out.mkdir(parents=True, exist_ok=True)
    Path('.cache').mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='release-source-',dir='.cache') as stage_name:
        stage=Path(stage_name).resolve()
        for directory in ['cmd','internal','docs','third_party','packaging/nix']:
            shutil.copytree(directory,stage/directory,ignore=shutil.ignore_patterns('rbtl3100s.dll'))
        for filename in ['go.mod','go.sum','LICENSE','NOTICE','README.md','VERSION']:
            shutil.copyfile(filename,stage/filename)
        build_snapshot(args,stage)


def build_snapshot(args,stage):
    module_dir = Path(subprocess.check_output(['go', 'list', '-m', '-f', '{{.Dir}}', 'google.golang.org/protobuf'], text=True).strip())
    modules = subprocess.check_output(['go', 'list', '-m', '-f', '{{.Path}} {{.Version}}', 'all'], text=True)
    assets = []
    nix_assets = {}
    digest=hashlib.sha256()
    for source in sorted(p for p in stage.rglob('*') if p.is_file()):
        digest.update(str(source.relative_to(stage)).encode()+b'\0'+source.read_bytes())
    source_digest=digest.hexdigest()
    toolchain = subprocess.check_output(['go', 'version'], text=True).strip()
    for goos, goarch in TARGETS:
        environment = dict(os.environ, CGO_ENABLED='0', GOOS=goos, GOARCH=goarch)
        # Do not inherit host tuning flags when cross-compiling.
        environment.pop('GOFLAGS', None)
        environment['GOARM'] = '7'
        for program in PROGRAMS:
            name = f'{program}-{args.version}-{goos}-{architecture(goarch)}'
            directory = args.out / name
            if directory.exists():
                shutil.rmtree(directory)
            directory.mkdir()
            executable = directory / (program + ('.exe' if goos == 'windows' else ''))
            subprocess.run(['go', 'build', '-buildvcs=false', '-trimpath', '-ldflags', f'-s -w -X main.version={args.version}', '-o', str(executable), './cmd/' + program], env=environment, cwd=stage, check=True)
            for source in NOTICES:
                shutil.copyfile(stage / source, directory / Path(source).name)
            shutil.copyfile(module_dir / 'LICENSE', directory / 'protobuf-LICENSE')
            (directory / 'modules.txt').write_text(modules)
            (directory / 'build.json').write_text(json.dumps({'version': args.version, 'program': program, 'goos': goos, 'goarch': goarch, 'goarm': '7' if goarch == 'arm' else None, 'cgo': False, 'source_sha256': source_digest, 'toolchain': toolchain, 'runtime_verified': False}, indent=2) + '\n')
            destination = args.out / (name + ('.zip' if goos == 'windows' else '.tar.gz'))
            archive(directory, destination, goos == 'windows')
            assets.append(destination)
            if goos != 'windows':
                system = {'amd64': 'x86_64', 'arm': 'armv7l', 'arm64': 'aarch64'}[goarch] + '-' + goos
                nix_assets.setdefault(program, {})[system] = {'file': destination.name, 'sha256': hashlib.sha256(destination.read_bytes()).hexdigest()}
            if args.packages and goos == 'linux':
                assets.extend(linux_packages(args, directory, program, goarch))
            print(f'BUILT {program} {goos}/{architecture(goarch)}', flush=True)
    assets.append(nix_package(args, stage, nix_assets))
    (args.out / 'SHA256SUMS').write_text(''.join(f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n' for p in sorted(assets)))


def linux_packages(args, directory, program, goarch):
    contents = [{'src': str(directory / program), 'dst': '/usr/bin/' + program, 'file_info': {'mode': 0o755}}]
    for path in sorted(p for p in directory.iterdir() if p.is_file() and p.name != program):
        contents.append({'src': str(path), 'dst': '/usr/share/doc/' + program + '/' + path.name, 'file_info': {'mode': 0o644}})
    config = {'name': program, 'arch': 'arm7' if goarch == 'arm' else goarch,
              'platform': 'linux', 'version': args.version.removeprefix('v'), 'release': '1',
              'maintainer': 'goTeddyCloud contributors', 'homepage': 'https://github.com/' + args.repository,
              'description': 'Go TeddyCloud server' if program == 'goteddycloud' else 'Native Go CC3200 serial and filesystem tool',
              'license': 'GPL-2.0-only', 'section': 'utils', 'priority': 'optional',
              'mtime': '1980-01-01T00:00:00Z', 'contents': contents}
    if program == 'goteddycloud':
        config['recommends'] = ['ffmpeg', 'ca-certificates']
    result = []
    with tempfile.TemporaryDirectory(prefix='nfpm-', dir='.cache') as temporary:
        path = Path(temporary) / 'nfpm.json'
        path.write_text(json.dumps(config))
        for packager, extension in [('deb', '.deb'), ('rpm', '.rpm'), ('archlinux', '.pkg.tar.zst')]:
            destination = args.out / f'{program}-{args.version}-linux-{architecture(goarch)}{extension}'
            subprocess.run([args.nfpm, 'package', '--config', str(path), '--packager', packager, '--target', str(destination)], check=True)
            result.append(destination)
    return result


def nix_package(args, stage, assets):
    directory = stage / ('goteddycloud-' + args.version + '-nix')
    directory.mkdir()
    (directory / 'assets.json').write_text(json.dumps(assets, indent=2) + '\n')
    shutil.copyfile(stage / 'packaging/nix/default.nix', directory / 'default.nix')
    shutil.copyfile(stage / 'packaging/nix/README.md', directory / 'README.md')
    shutil.copyfile(stage / 'LICENSE', directory / 'LICENSE')
    (directory / 'release.json').write_text(json.dumps({'version': args.version, 'repository': args.repository}) + '\n')
    destination = args.out / (directory.name + '.tar.gz')
    archive(directory, destination, False)
    return destination


if __name__ == '__main__':
    main()
