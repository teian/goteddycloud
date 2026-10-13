#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Snapshot the current official release catalogs at one immutable Git commit."""
import hashlib
import json
from pathlib import Path
import re
import sys
import urllib.request


def main():
    reference = json.loads(Path(sys.argv[1]).read_bytes())
    revision = reference['object']['sha']
    if not re.fullmatch(r'[0-9a-f]{40}', revision):
        raise SystemExit('Invalid catalog Git revision')
    output = Path(sys.argv[2])
    output.mkdir(parents=True, exist_ok=True)
    origin = 'https://raw.githubusercontent.com/toniebox-reverse-engineering/tonies-json/'
    manifest = {'repository': 'toniebox-reverse-engineering/tonies-json',
                'revision': revision, 'files': {}}
    payloads = {}
    for name in ('tonies.json', 'tonieboxes.json'):
        with urllib.request.urlopen(origin + revision + '/' + name, timeout=30) as response:
            data = response.read((64 << 20) + 1)
        if len(data) > 64 << 20:
            raise SystemExit(f'{name} exceeds size limit')
        entries = json.loads(data)
        if not isinstance(entries, list) or not entries or not all(isinstance(e, dict) for e in entries):
            raise SystemExit(f'{name} must contain a nonempty array of objects')
        payloads[name] = data
        manifest['files'][name] = {'sha256': hashlib.sha256(data).hexdigest(), 'entries': len(entries)}
    for name, data in payloads.items():
        (output / name).write_bytes(data)
    (output / 'snapshot.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(json.dumps(manifest))


if __name__ == '__main__':
    main()
