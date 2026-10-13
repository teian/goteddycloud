#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Compare OCI exports and require a server/UI change to replace only its layer."""
import argparse
import io
import json
import tarfile


def image(archive, platform):
    def blob(digest):
        algorithm, value = digest.split(':', 1)
        assert algorithm == 'sha256' and len(value) == 64
        return archive.extractfile('blobs/sha256/' + value).read()

    found = {}

    def visit(descriptor):
        if descriptor.get('annotations', {}).get('vnd.docker.reference.type') == 'attestation-manifest':
            return
        manifest = json.loads(blob(descriptor['digest']))
        if 'manifests' in manifest:
            for child in manifest['manifests']:
                visit(child)
            return
        config = json.loads(blob(manifest['config']['digest']))
        name = config['os'] + '/' + config['architecture']
        if name != platform:
            return
        found[descriptor['digest']] = (manifest, config)

    for descriptor in json.load(archive.extractfile('index.json'))['manifests']:
        visit(descriptor)
    assert len(found) == 1, f'expected one distinct {platform} image, found {len(found)}'
    manifest, config = next(iter(found.values()))
    return manifest, config, blob


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('before')
    parser.add_argument('after')
    parser.add_argument('--platform', default='linux/amd64')
    parser.add_argument('--changed-payload', choices=['server', 'frontend'], default='server')
    args = parser.parse_args()
    with tarfile.open(args.before) as before, tarfile.open(args.after) as after:
        old, old_config, _ = image(before, args.platform)
        new, new_config, blob = image(after, args.platform)
        for key in ('User', 'Env', 'Entrypoint', 'Cmd', 'WorkingDir', 'Volumes', 'ExposedPorts'):
            assert old_config['config'].get(key) == new_config['config'].get(key), f'runtime config changed: {key}'
        assert len(old['layers']) == len(new['layers']), 'layer count changed'
        changed = [i for i, (a, b) in enumerate(zip(old['layers'], new['layers'])) if a['digest'] != b['digest']]
        assert len(changed) == 1, f'expected one changed payload layer, found {changed}'
        index = changed[0]
        assert old_config['rootfs']['diff_ids'][index] != new_config['rootfs']['diff_ids'][index], 'only compression changed'
        with tarfile.open(fileobj=io.BytesIO(blob(new['layers'][index]['digest']))) as layer:
            files = [member.name.removeprefix('./') for member in layer if not member.isdir()]
        assert files, 'empty changed layer'
        if args.changed_payload == 'server':
            assert files == ['usr/local/bin/goteddycloud'], f'unrelated files in server layer: {files}'
        else:
            assert all(path.startswith('usr/share/goteddycloud/www/web/') for path in files), 'unrelated files in UI layer'
        reused = sum(layer['size'] for i, layer in enumerate(new['layers']) if i != index)
        download = new['layers'][index]['size']
        print(f'PASS {args.changed_payload} change reuses {len(new["layers"]) - 1}/{len(new["layers"])} compressed layers')
        print(f'Reused {reused:,} bytes; changed payload download {download:,} bytes (plus image metadata)')


if __name__ == '__main__':
    main()
