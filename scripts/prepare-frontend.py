#!/usr/bin/env python3
"""Stage an isolated copy of the frontend pinned in frontend/upstream-revision."""
from pathlib import Path
import shutil
import sys

root = Path(__file__).resolve().parent.parent
source = Path(sys.argv[1]).resolve()
stage = root / '.cache/goteddycloud-web'
marker = stage / '.goteddycloud-overlay'
if stage.exists():
    if not marker.is_file():
        raise SystemExit('Refusing to replace an unmarked frontend stage')
    shutil.rmtree(stage)
shutil.copytree(source, stage, ignore=shutil.ignore_patterns('.git', 'node_modules', 'dist'))
marker.write_text('Generated from the pinned frontend fork; edit ~/code/teddycloud_web instead.\n')
print(stage)
