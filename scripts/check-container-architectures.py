#!/usr/bin/env python3
"""Check the Go executable inside every platform in a Buildx OCI archive."""
import argparse
import io
import json
import struct
import tarfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('archive')
    args = parser.parse_args()
    expected = {('linux', 'amd64'): (2, 62), ('linux', 'arm'): (1, 40), ('linux', 'arm64'): (2, 183)}
    found = set()
    with tarfile.open(args.archive) as archive:
        def blob(digest):
            algorithm, value = digest.split(':', 1)
            assert algorithm == 'sha256' and len(value) == 64
            return archive.extractfile('blobs/sha256/' + value).read()

        def inspect(descriptor):
            if descriptor.get('annotations', {}).get('vnd.docker.reference.type') == 'attestation-manifest':
                # Provenance/SBOM payloads have an empty image config, not an OS.
                return
            manifest = json.loads(blob(descriptor['digest']))
            if 'manifests' in manifest:
                for child in manifest['manifests']:
                    inspect(child)
                return
            config = json.loads(blob(manifest['config']['digest']))
            platform = (config['os'], config['architecture'])
            assert platform in expected and platform not in found, platform
            metadata = descriptor.get('platform', {})
            assert (metadata.get('os'), metadata.get('architecture')) == platform, 'manifest/config platform mismatch'
            if platform[1] == 'arm':
                assert metadata.get('variant', config.get('variant')) == 'v7', 'ARM must be ARMv7'
            executable = None
            for layer in manifest['layers']:
                with tarfile.open(fileobj=io.BytesIO(blob(layer['digest']))) as files:
                    for member in files.getmembers():
                        if member.name.removeprefix('./') == 'usr/local/bin/goteddycloud':
                            assert member.isfile(), 'executable must be a regular file'
                            executable = files.extractfile(member).read(20)
            assert executable is not None and executable[:4] == b'\x7fELF', platform
            assert executable[5] == 1, 'expected little-endian ELF'
            actual = (executable[4], struct.unpack_from('<H', executable, 18)[0])
            assert actual == expected[platform], f'{platform}: executable {actual} != {expected[platform]}'
            found.add(platform)
            print('VERIFIED', '/'.join(platform), 'ELF class/machine', actual)

        for descriptor in json.load(archive.extractfile('index.json'))['manifests']:
            inspect(descriptor)
    assert found == set(expected), 'missing platform images'
    print('PASS all three container image architectures match their executables')


if __name__ == '__main__':
    main()
