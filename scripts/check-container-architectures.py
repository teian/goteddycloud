#!/usr/bin/env python3
"""Check Alpine runtime contents and executable architectures in a Buildx OCI archive."""
import argparse
import hashlib
import io
import json
import re
import struct
import tarfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('archive')
    parser.add_argument('--require-ffmpeg', action='store_true')
    parser.add_argument('--require-catalog', action='store_true')
    args = parser.parse_args()
    expected = {('linux', 'amd64'): (2, 62), ('linux', 'arm'): (1, 40), ('linux', 'arm64'): (2, 183)}
    found = {}
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
            assert platform in expected, platform
            metadata = descriptor.get('platform', {})
            assert (metadata.get('os'), metadata.get('architecture')) == platform, 'manifest/config platform mismatch'
            if platform[1] == 'arm':
                assert metadata.get('variant', config.get('variant')) == 'v7', 'ARM must be ARMv7'
            if platform in found:
                # OCI exports can reference one image index once per tag (for
                # example v0.1.0 and latest). Accept identical manifest aliases,
                # while still rejecting two different images for one platform.
                assert found[platform] == descriptor['digest'], 'conflicting platform manifests'
                return
            executable = ffmpeg = alpine_version = None
            catalogs = {}
            forbidden = {
                'etc/debian_version', 'usr/bin/git', 'usr/bin/python3',
                'usr/bin/node', 'usr/local/bin/node', 'usr/local/go/bin/go',
                'usr/bin/gcc', 'usr/bin/cc',
            }
            for layer in manifest['layers']:
                with tarfile.open(fileobj=io.BytesIO(blob(layer['digest']))) as files:
                    for member in files.getmembers():
                        path = member.name.removeprefix('./')
                        assert path not in forbidden, f'unnecessary runtime file: {path}'
                        if path == 'etc/alpine-release':
                            alpine_version = files.extractfile(member).read(32).decode().strip()
                        if path == 'usr/bin/ffmpeg':
                            assert member.isfile(), 'FFmpeg must be a regular file'
                            ffmpeg = files.extractfile(member).read(20)
                        if path == 'usr/local/bin/goteddycloud':
                            assert member.isfile(), 'executable must be a regular file'
                            executable = files.extractfile(member).read(20)
                        if path in ('usr/share/goteddycloud/catalog/tonies.json',
                                    'usr/share/goteddycloud/catalog/tonieboxes.json',
                                    'usr/share/goteddycloud/catalog/snapshot.json'):
                            assert member.isfile() and member.size <= 64 << 20, 'invalid catalog file'
                            catalogs[path.rsplit('/', 1)[1]] = files.extractfile(member).read()
            assert executable is not None and executable[:4] == b'\x7fELF', platform
            assert executable[5] == 1, 'expected little-endian ELF'
            actual = (executable[4], struct.unpack_from('<H', executable, 18)[0])
            assert actual == expected[platform], f'{platform}: executable {actual} != {expected[platform]}'
            assert alpine_version, f'{platform}: Alpine base is required'
            if args.require_catalog:
                assert set(catalogs) == {'tonies.json', 'tonieboxes.json', 'snapshot.json'}, 'catalog baseline missing'
                snapshot = json.loads(catalogs['snapshot.json'])
                assert snapshot['repository'] == 'toniebox-reverse-engineering/tonies-json'
                assert re.fullmatch('[0-9a-f]{40}', snapshot['revision']), 'invalid catalog revision'
                for name in ('tonies.json', 'tonieboxes.json'):
                    data = catalogs[name]
                    entries = json.loads(data)
                    assert isinstance(entries, list) and entries and all(isinstance(e, dict) for e in entries)
                    assert snapshot['files'][name] == {'entries': len(entries), 'sha256': hashlib.sha256(data).hexdigest()}
                assert 'GOTEDDYCLOUD_BUNDLED_CATALOG=/usr/share/goteddycloud/catalog' in config['config']['Env']
            if args.require_ffmpeg:
                assert ffmpeg is not None, f'{platform}: FFmpeg missing'
            if ffmpeg is not None:
                assert ffmpeg[:4] == b'\x7fELF' and ffmpeg[5] == 1, 'invalid FFmpeg ELF'
                actual_ffmpeg = (ffmpeg[4], struct.unpack_from('<H', ffmpeg, 18)[0])
                assert actual_ffmpeg == expected[platform], f'{platform}: FFmpeg architecture mismatch'
            found[platform] = descriptor['digest']
            compressed = sum(layer['size'] for layer in manifest['layers']) / (1024 * 1024)
            print('VERIFIED', '/'.join(platform), 'Alpine', alpine_version, 'ELF class/machine', actual,
                  f'download layers {compressed:.2f} MiB')

        for descriptor in json.load(archive.extractfile('index.json'))['manifests']:
            inspect(descriptor)
    assert set(found) == set(expected), 'missing platform images'
    print('PASS all three container image architectures match their executables')


if __name__ == '__main__':
    main()
