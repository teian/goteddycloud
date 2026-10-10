#!/usr/bin/env python3
"""Run pinned upstream custom JSON tests against a disposable Go deployment."""
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

PIN = '78c3b38e36f665d3a5fffe907f73a02725176377'

def load(name,path):
    spec=importlib.util.spec_from_file_location(name,path)
    module=importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

def main():
    reference=Path('.cache/teddycloud-reference').resolve()
    assert subprocess.check_output(['git','-C',str(reference),'rev-parse','HEAD'],text=True).strip()==PIN
    module=load('upstream_custom_tests',reference/'tests/test_tonies_custom_json_api.py')
    native=load('native_smoke',Path('scripts/native-smoke.py'))
    Path('.cache').mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='custom-api-',dir='.cache') as directory:
        root=Path(directory).resolve()
        deployment=root/'deployment'
        binary=root/'goteddycloud'
        subprocess.run(['go','build','-buildvcs=false','-o',str(binary),'./cmd/goteddycloud'],env=dict(os.environ,CGO_ENABLED='0'),check=True)
        subprocess.run(['go','run','-buildvcs=false','./cmd/fixturegen','-output',str(deployment)],check=True)
        assert (deployment/'.synthetic-fixture').is_file()
        process,addresses=native.start(binary,deployment)
        try:
            module.BASE_URL='http://'+addresses['http']
            suite=unittest.TestSuite()
            suite.addTest(module.ToniesCustomJsonBackupTests('test_backup_creation_and_rotation'))
            for name in ['test_save_many_entries_and_verify_read_and_search','test_save_multiple_entries_and_readback_and_reload','test_validation_blocks_wrong_values','test_delete_and_rename_endpoints']:
                suite.addTest(module.ToniesCustomJsonApiTests(name))
            result=unittest.TextTestRunner(verbosity=2).run(suite)
            if not result.wasSuccessful(): return 1
            print('PASS pinned upstream custom JSON backup/bulk/normalization/validation/rename tests')
        finally:
            native.stop(process,addresses)
    return 0

if __name__=='__main__':
    sys.exit(main())
