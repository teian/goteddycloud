#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Retain upstream attribution and installed npm dependency license notices."""
import json
from pathlib import Path
import shutil
import sys

source = Path(sys.argv[1]).resolve()
output = Path(sys.argv[2]).resolve()
output.mkdir(parents=True, exist_ok=True)
for name in ('README.md', 'package.json', 'package-lock.json',
             'src/assets/fonts/teddystudio/LICENSES.md'):
    destination = output / name
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(source / name, destination)

root = Path(__file__).resolve().parent.parent
shutil.copyfile(root / 'frontend/upstream-revision', output / 'upstream-revision')
packages = json.loads((source / 'package-lock.json').read_text())['packages']
inventory = []
for location in sorted(packages):
    if not location.startswith('node_modules/'):
        continue
    directory = source / location
    manifest = directory / 'package.json'
    if not manifest.is_file():  # Optional packages for other build platforms.
        continue
    metadata = json.loads(manifest.read_text())
    notices = []
    for path in sorted(directory.iterdir()):
        if path.is_file() and path.name.lower().startswith(('license', 'licence', 'copying', 'notice')):
            destination = output / location / path.name
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(path, destination)
            notices.append(str(destination.relative_to(output)))
    inventory.append({
        'name': metadata.get('name'), 'version': metadata.get('version'),
        'license': metadata.get('license', metadata.get('licenses')),
        'repository': metadata.get('repository'), 'notices': notices,
    })
(output / 'npm-packages.json').write_text(json.dumps(inventory, indent=2) + '\n')
