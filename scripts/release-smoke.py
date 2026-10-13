#!/usr/bin/env python3
"""Inspect release archives and Linux packages without installing them."""
import argparse
import hashlib
import json
from pathlib import Path
import struct
import subprocess
import tarfile
import zipfile


def command(*args):
    return subprocess.check_output([str(a) for a in args])


def check_executable(data, system, arch):
    if system == 'linux':
        assert data[:4] == b'\x7fELF'
        assert data[4] == (1 if arch == 'armv7' else 2)
        assert struct.unpack_from('<H', data, 18)[0] == {'amd64': 62, 'armv7': 40, 'arm64': 183}[arch]
    elif system == 'windows':
        assert data[:2] == b'MZ'
        offset = struct.unpack_from('<I', data, 0x3c)[0]
        assert data[offset:offset + 4] == b'PE\0\0'
        assert struct.unpack_from('<H', data, offset + 4)[0] == {'amd64': 0x8664, 'arm64': 0xaa64}[arch]
    else:
        assert data[:4] == b'\xcf\xfa\xed\xfe'
        assert struct.unpack_from('<I', data, 4)[0] == {'amd64': 0x1000007, 'arm64': 0x100000c}[arch]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    parser.add_argument('version')
    args = parser.parse_args()
    root, version = args.directory.resolve(), args.version
    checksums = {}
    for row in (root / 'SHA256SUMS').read_text().splitlines():
        digest, name = row.split('  ', 1)
        assert name not in checksums and Path(name).name == name
        checksums[name] = digest
        assert hashlib.sha256((root / name).read_bytes()).hexdigest() == digest, name
    assert set(checksums) == {p.name for p in root.iterdir() if p.is_file() and p.name != 'SHA256SUMS'}
    targets = [('linux', 'amd64'), ('linux', 'armv7'), ('linux', 'arm64'),
               ('windows', 'amd64'), ('windows', 'arm64'), ('darwin', 'amd64'), ('darwin', 'arm64')]
    expected = {f'goteddycloud-{version}-nix.tar.gz'}
    package_count = 0
    for program in ['goteddycloud', 'cc3200tool']:
        for system, arch in targets:
            name = f'{program}-{version}-{system}-{arch}'
            suffix = '.zip' if system == 'windows' else '.tar.gz'
            path = root / (name + suffix)
            expected.add(path.name)
            executable = program + ('.exe' if system == 'windows' else '')
            if system == 'windows':
                with zipfile.ZipFile(path) as archive:
                    assert archive.testzip() is None
                    names = archive.namelist()
                    data = archive.read(name + '/' + executable)
                    build = json.loads(archive.read(name + '/build.json'))
            else:
                with tarfile.open(path) as archive:
                    names = archive.getnames()
                    assert archive.getmember(name + '/' + executable).mode == 0o755
                    data = archive.extractfile(name + '/' + executable).read()
                    build = json.load(archive.extractfile(name + '/build.json'))
            check_executable(data, system, arch)
            assert build['program'] == program and build['version'] == version and not build['cgo']
            assert not any(n.endswith('.dll') for n in names), 'manufacturer DLL in release'
            for notice in ['LICENSE', 'NOTICE', 'licenses.md', 'Go-LICENSE', 'go-serial-LICENSE', 'kong-COPYING', 'x-term-LICENSE']:
                assert name + '/' + notice in names, (name, notice)
            if system != 'linux':
                continue
            for packager, extension, package_arch in [
                ('deb', '.deb', {'amd64': 'amd64', 'armv7': 'armhf', 'arm64': 'arm64'}),
                ('rpm', '.rpm', {'amd64': 'x86_64', 'armv7': 'armv7hl', 'arm64': 'aarch64'}),
                ('archlinux', '.pkg.tar.zst', {'amd64': 'x86_64', 'armv7': 'armv7h', 'arm64': 'aarch64'}),
            ]:
                package = root / (name + extension)
                expected.add(package.name)
                if packager == 'deb':
                    assert command('dpkg-deb', '-f', package, 'Architecture').decode().strip() == package_arch[arch]
                    payload = command('dpkg-deb', '--fsys-tarfile', package)
                    import io
                    with tarfile.open(fileobj=io.BytesIO(payload)) as archive:
                        packaged = archive.extractfile('./usr/bin/' + program).read()
                        assert './usr/share/doc/' + program + '/LICENSE' in archive.getnames()
                elif packager == 'rpm':
                    assert command('rpm', '-qp', '--qf', '%{ARCH}', package).decode().strip() == package_arch[arch]
                    files = command('rpm', '-qpl', package).decode().splitlines()
                    assert '/usr/bin/' + program in files
                    assert '/usr/share/doc/' + program + '/LICENSE' in files
                    assert command('rpm', '-qp', '--scripts', package).strip() == b''
                    # Read cpio without installing or extracting into the host filesystem.
                    payload = command('rpm2cpio', package)
                    packaged = subprocess.check_output(['bsdtar', '-xOf', '-', './usr/bin/' + program], input=payload)
                else:
                    info = command('bsdtar', '-xOf', package, '.PKGINFO').decode()
                    assert f'arch = {package_arch[arch]}\n' in info
                    packaged = command('bsdtar', '-xOf', package, 'usr/bin/' + program)
                    assert 'usr/share/doc/' + program + '/LICENSE' in command('bsdtar', '-tf', package).decode().splitlines()
                assert packaged == data, 'packaged executable differs from native archive'
                package_count += 1
    assert set(checksums) == expected, 'missing or unexpected release assets'
    with tarfile.open(root / f'goteddycloud-{version}-nix.tar.gz') as archive:
        prefix = f'goteddycloud-{version}-nix/'
        assets = json.load(archive.extractfile(prefix + 'assets.json'))
        release = json.load(archive.extractfile(prefix + 'release.json'))
        assert release['version'] == version
        for program, systems in assets.items():
            assert program in ['goteddycloud', 'cc3200tool'] and len(systems) == 5
            for asset in systems.values():
                assert checksums[asset['file']] == asset['sha256']
    server = root / f'goteddycloud-{version}-linux-amd64/goteddycloud'
    assert command(server, '-version').decode().strip() == 'goteddycloud ' + version
    tool = root / f'cc3200tool-{version}-linux-amd64/cc3200tool'
    subprocess.run([str(tool), '--help'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    print(f'PASS 14 native archives, {package_count} Linux packages, Nix download hashes, complete checksums and native executable smoke')


if __name__ == '__main__':
    main()
