#!/usr/bin/env python3
"""Build versioned, cgo-free backend archives without publishing releases."""
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

TARGETS = [('linux', 'amd64'), ('linux', 'arm64'), ('windows', 'amd64'), ('darwin', 'amd64'), ('darwin', 'arm64')]


def archive(directory, destination, windows):
    if windows:
        with zipfile.ZipFile(destination, 'w', compression=zipfile.ZIP_DEFLATED) as output:
            for file in sorted(directory.iterdir()):
                info = zipfile.ZipInfo(f'{directory.name}/{file.name}', date_time=(1980, 1, 1, 0, 0, 0))
                info.external_attr = (file.stat().st_mode & 0xffff) << 16
                output.writestr(info, file.read_bytes(), compress_type=zipfile.ZIP_DEFLATED)
    else:
        with destination.open('wb') as raw, gzip.GzipFile(filename='', fileobj=raw, mode='wb', mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode='w') as output:
                for file in sorted(directory.iterdir()):
                    data = file.read_bytes()
                    info = tarfile.TarInfo(f'{directory.name}/{file.name}')
                    info.size = len(data)
                    info.mode = 0o755 if file.name == 'goteddycloud' else 0o644
                    info.mtime = 0
                    output.addfile(info, io.BytesIO(data))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('version')
    parser.add_argument('--out', type=Path, default=Path('dist'))
    args = parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._+-]{0,79}', args.version):
        parser.error('version must be a safe identifier')
    args.out = args.out.resolve()
    args.out.mkdir(parents=True, exist_ok=True)
    Path('.cache').mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='release-source-',dir='.cache') as stage_name:
        stage=Path(stage_name).resolve()
        for directory in ['cmd','internal','docs','third_party']:
            shutil.copytree(directory,stage/directory)
        for filename in ['go.mod','go.sum','LICENSE','NOTICE','README.md']:
            shutil.copyfile(filename,stage/filename)
        build_snapshot(args,stage)


def build_snapshot(args,stage):
    module_dir = Path(subprocess.check_output(['go', 'list', '-m', '-f', '{{.Dir}}', 'google.golang.org/protobuf'], text=True).strip())
    modules = subprocess.check_output(['go', 'list', '-m', '-f', '{{.Path}} {{.Version}}', 'all'], text=True)
    checksums = []
    digest=hashlib.sha256()
    for source in sorted(p for p in stage.rglob('*') if p.is_file()):
        digest.update(str(source.relative_to(stage)).encode()+b'\0'+source.read_bytes())
    source_digest=digest.hexdigest()
    for goos, goarch in TARGETS:
        name = f'goteddycloud-{args.version}-{goos}-{goarch}'
        directory = args.out / name
        directory.mkdir(exist_ok=True)
        executable = directory / ('goteddycloud.exe' if goos == 'windows' else 'goteddycloud')
        environment = dict(os.environ, CGO_ENABLED='0', GOOS=goos, GOARCH=goarch)
        subprocess.run(['go', 'build', '-buildvcs=false', '-trimpath', '-ldflags', f'-s -w -X main.version={args.version}', '-o', str(executable), './cmd/goteddycloud'], env=environment, cwd=stage, check=True)
        for source in ['LICENSE', 'NOTICE', 'README.md', 'docs/licenses.md', 'docs/storage.md', 'docs/storage-verification.md', 'docs/s3-providers.md', 'docs/cc3200-flashing.md', 'docs/cc3200-tool.md', 'docs/reverse-proxy.md', 'docs/management-tls.md', 'docs/user-areas.md', 'docs/migration.md', 'docs/verification.md', 'third_party/Go-LICENSE', 'third_party/go-serial-LICENSE', 'third_party/x-sys-LICENSE', 'third_party/x-crypto-LICENSE', 'third_party/x-net-LICENSE', 'third_party/x-text-LICENSE', 'third_party/libopus-COPYING', 'third_party/gofakes3-LICENSE', 'third_party/goskiplist-LICENSE']:
            shutil.copyfile(stage / source, directory / Path(source).name)
        shutil.copyfile(module_dir / 'LICENSE', directory / 'protobuf-LICENSE')
        (directory / 'modules.txt').write_text(modules)
        (directory / 'build.json').write_text(json.dumps({'version': args.version, 'goos': goos, 'goarch': goarch, 'cgo': False, 'source_sha256':source_digest, 'toolchain': subprocess.check_output(['go', 'version'], text=True).strip(), 'runtime_verified': False}, indent=2) + '\n')
        destination = args.out / (name + ('.zip' if goos == 'windows' else '.tar.gz'))
        archive(directory, destination, goos == 'windows')
        checksums.append(f'{hashlib.sha256(destination.read_bytes()).hexdigest()}  {destination.name}')
        print(f'BUILT {goos}/{goarch}', flush=True)
    (args.out / 'SHA256SUMS').write_text('\n'.join(checksums) + '\n')


if __name__ == '__main__':
    main()
