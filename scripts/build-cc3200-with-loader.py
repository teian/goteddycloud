#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Build local native binaries with a user-supplied CC3200 loader."""
import argparse
import hashlib
import os
from pathlib import Path
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('loader', type=Path, help='Local stock rbtl3100s.dll')
    parser.add_argument('--out', type=Path, default=Path('bin'))
    args = parser.parse_args()
    data = args.loader.read_bytes()
    # Match the protocol core's pinned stock loader.
    expected = '2c9a05c469389446e05871a88e84ca3994bf3d6a6785155135db21d33aa63cc9'
    if hashlib.sha256(data).hexdigest() != expected:
        parser.error('loader SHA256 does not match the pinned stock rbtl3100s.dll')
    root = Path(__file__).resolve().parents[1]
    asset = root / 'internal/cc3200cli/loader/rbtl3100s.dll'
    asset.parent.mkdir(parents=True, exist_ok=True)
    asset.write_bytes(data)
    asset.chmod(0o600)
    output = args.out.resolve()
    output.mkdir(parents=True, exist_ok=True)
    environment = dict(os.environ, CGO_ENABLED='0')
    built = []
    suffix = '.exe' if environment.get('GOOS', '') == 'windows' or os.name == 'nt' else ''
    for name in ('cc3200tool', 'goteddycloud'):
        destination = output / (name + suffix)
        temporary = output / (name + suffix + '.next')
        subprocess.run(['go', 'build', '-buildvcs=false', '-tags=cc3200_embedded_loader',
                        '-o', str(temporary), './cmd/' + name], cwd=root, env=environment, check=True)
        built.append((temporary, destination))
    for temporary, destination in built:
        os.replace(temporary, destination)
        print('Built with local embedded loader:', destination)


if __name__ == '__main__':
    main()
