#!/usr/bin/env python3
"""Run a private disposable container and verify persistent codec-free delivery."""
import argparse
import json
from pathlib import Path
import subprocess
import time
import urllib.request
import uuid


def docker(*args, **kwargs):
    return subprocess.check_output(['docker', *args], **kwargs)


def ready(name):
    state = json.loads(docker('inspect', name, text=True))[0]
    bindings = state['NetworkSettings']['Ports']['18080/tcp']
    origin = 'http://127.0.0.1:' + bindings[0]['HostPort']
    for _ in range(50):
        try:
            with urllib.request.urlopen(origin + '/v1/time', timeout=1) as response:
                assert response.read().isdigit()
                return origin
        except OSError:
            time.sleep(0.1)
    raise AssertionError('container readiness deadline exceeded')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--image', default='goteddycloud:local')
    parser.add_argument('--taf', type=Path, default=Path('.cache/oracle-runtime/data/library/go-grouped.taf'))
    args = parser.parse_args()
    name = 'goteddycloud-smoke-' + uuid.uuid4().hex[:12]
    try:
        docker('run', '-d', '--name', name, '-p', '127.0.0.1::18080', args.image)
        origin = ready(name)
        key = docker('exec', name, 'sha256sum', '/data/certs/server/teddy-key.pem')
        target = origin + '/api/settings/set/toniebox.max_vol_spk'
        with urllib.request.urlopen(urllib.request.Request(target, data=b'2', method='POST'), timeout=3) as response:
            assert response.status == 200
        docker('exec', name, 'mkdir', '-p', '/data/data/content/default/00000001')
        subprocess.run(['docker', 'exec', '-i', name, 'sh', '-c', 'cat > /data/data/content/default/00000001/00000001'], input=args.taf.read_bytes(), check=True)
        with urllib.request.urlopen(origin + '/v1/content/0000000100000001', timeout=3) as response:
            assert response.read() == args.taf.read_bytes(), 'codec-free stored delivery mismatch'
        docker('stop', '-t', '5', name)
        assert docker('wait', name, text=True).strip() == '0', 'container required forced termination'
        docker('start', name)
        origin = ready(name)
        with urllib.request.urlopen(origin + '/api/settings/get/toniebox.max_vol_spk', timeout=3) as response:
            assert response.read() == b'2', 'setting lost on restart'
        assert docker('exec', name, 'sha256sum', '/data/certs/server/teddy-key.pem') == key, 'container regenerated identity'
        docker('stop', '-t', '5', name)
        assert docker('wait', name, text=True).strip() == '0'
    finally:
        subprocess.run(['docker', 'rm', '-fv', name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    print('PASS Linux container startup, codec-free TAF bytes, persistent settings/identities, restart, and shutdown')


if __name__ == '__main__':
    main()
