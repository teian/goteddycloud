#!/usr/bin/env python3
"""Rehearse validation, mutation, and rollback on new synthetic copies only."""
import argparse
import hashlib
import shutil
import subprocess
from pathlib import Path
import tempfile


def hashes(root):
    return {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in root.rglob('*') if p.is_file()}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', type=Path, default=Path('bin/goteddycloud-next'))
    args = parser.parse_args()
    binary = args.binary.resolve()
    Path('.cache').mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='migration-', dir='.cache') as temporary:
        base = Path(temporary)
        copied = base / 'deployment'
        snapshot = base / 'snapshot'
        subprocess.run(['go', 'run', '-buildvcs=false', './cmd/fixturegen', '-output', str(copied)], check=True)
        assert (copied / '.synthetic-fixture').is_file()
        shutil.copytree(copied, snapshot)
        baseline = hashes(copied)
        subprocess.run([str(binary), '-base_path', str(copied), '-validate'], check=True)
        assert hashes(copied) == baseline, 'read-only validation modified copied data'
        subprocess.run([str(binary), '-base_path', str(copied), '-config-set', 'toniebox.max_vol_spk=2'], check=True)
        assert hashes(copied) != baseline, 'mutation did not persist'
        subprocess.run([str(binary), '-base_path', str(copied), '-validate'], check=True)
        # No writer is running. Restore the pre-cutover tree, including identities.
        shutil.rmtree(copied)
        shutil.copytree(snapshot, copied)
        assert hashes(copied) == baseline, 'rollback changed file contents'
        subprocess.run([str(binary), '-base_path', str(copied), '-validate'], check=True)
    print('PASS read-only migration validation, persisted mutation, and exact snapshot restoration')


if __name__ == '__main__':
    main()
