#!/usr/bin/env python3
"""Native startup/settings/restart/TLS/shutdown checks on synthetic data only."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import signal
import ssl
import subprocess
import tempfile
import time
import urllib.request
import urllib.error


def start(binary, root):
    process = subprocess.Popen([str(binary), '-base_path', str(root), '-http', '127.0.0.1:0', '-httpsecure', '127.0.0.1:0', '-box', '127.0.0.1:0'], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    return process, ready(process)


def ready(process):
    selector = selectors.DefaultSelector()
    selector.register(process.stderr, selectors.EVENT_READ)
    deadline = time.monotonic() + 10
    try:
        while time.monotonic() < deadline:
            if selector.select(0.2):
                line = process.stderr.readline()
                if 'listeners ready' in line:
                    addresses = dict(re.findall(r'(http|web|box):([0-9.:]+)', line))
                    assert len(addresses) == 3, 'missing listener readiness'
                    return addresses
            if process.poll() is not None:
                raise AssertionError('native startup failed')
        raise AssertionError('readiness deadline exceeded')
    except BaseException:
        process.kill()
        process.wait()
        raise
    finally:
        selector.close()


def stop(process, addresses):
    children = []
    if Path('/proc').is_dir():
        for entry in Path('/proc').iterdir():
            try:
                if f'PPid:\t{process.pid}\n' in (entry / 'status').read_text():
                    children.append(entry)
            except (OSError, UnicodeDecodeError):
                pass
    process.send_signal(signal.SIGTERM)
    assert process.wait(timeout=5) == 0, 'shutdown failed'
    process.stdout.close()
    process.stderr.close()
    assert not any(child.exists() for child in children), 'worker child survived parent'


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', type=Path, default=Path('bin/goteddycloud'))
    args = parser.parse_args()
    binary = args.binary.resolve()
    Path('.cache').mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='native-smoke-', dir='.cache') as temporary:
        root = Path(temporary).resolve() / 'fixture'
        subprocess.run(['go', 'run', '-buildvcs=false', './cmd/fixturegen', '-output', str(root)], check=True)
        cert_hashes = {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest() for p in (root / 'certs').rglob('*') if p.is_file()}
        context = ssl.create_default_context(cafile=str(root / 'certs/server/teddy-cert.pem'))
        context.load_cert_chain(root / 'certs/client/001122334455/teddy-cert.pem', root / 'certs/client/001122334455/teddy-key.pem')
        for restart in range(2):
            process, addresses = start(binary, root)
            try:
                for listener in ['http', 'web', 'box']:
                    scheme = 'http' if listener == 'http' else 'https'
                    endpoint = '/v1/time' if listener == 'box' else '/api/auth/status'
                    with urllib.request.urlopen(f'{scheme}://{addresses[listener]}{endpoint}', context=context, timeout=3) as response:
                        assert response.status == 200
                        if listener == 'box':
                            assert response.read().isdigit()
                        else:
                            assert isinstance(json.load(response), dict)
                    if listener != 'box':
                        try:
                            urllib.request.urlopen(f'{scheme}://{addresses[listener]}/v1/time', context=context, timeout=3)
                        except urllib.error.HTTPError as error:
                            assert error.code == 404, 'management exposed box API'
                            error.close()
                        else:
                            raise AssertionError('management exposed box API')
                path = f'http://{addresses["http"]}/api/settings/'
                with urllib.request.urlopen(path + 'get/toniebox.max_vol_spk?overlay=001122334455', timeout=3) as response:
                    assert response.read() == (b'1' if restart == 0 else b'2'), 'overlay restart mismatch'
                if restart == 0:
                    request = urllib.request.Request(path + 'set/toniebox.max_vol_spk?overlay=001122334455', data=b'2', method='POST')
                    with urllib.request.urlopen(request, timeout=3) as response:
                        assert response.status == 200 and response.read() == b''
                    with urllib.request.urlopen(f'http://{addresses["http"]}/api/triggerRestart', timeout=3) as response:
                        assert response.read() == b'OK'
                    addresses = ready(process)
                    assert process.poll() is None, 'restart replaced/exited supervisor'
                    with urllib.request.urlopen(f'http://{addresses["http"]}/api/settings/get/toniebox.max_vol_spk?overlay=001122334455', timeout=3) as response:
                        assert response.read() == b'2', 'trigger restart lost settings'

            finally:
                stop(process, addresses)
        after = {name: hashlib.sha256((root / name).read_bytes()).hexdigest() for name in cert_hashes}
        assert cert_hashes == after, 'restart changed imported identities'
        subprocess.run([str(binary), '-base_path', str(root), '-validate'], check=True)
    print('PASS native listeners, verified TLS, persisted overlays, restart identity preservation, and child shutdown')


if __name__ == '__main__':
    main()
