#!/usr/bin/env python3
"""Verify private management areas and owned-device routing in the native process."""
import hashlib
import importlib.util
import json
from pathlib import Path
import ssl
import subprocess
import tempfile
import urllib.error
import urllib.request

spec = importlib.util.spec_from_file_location('native_smoke', Path(__file__).with_name('native-smoke.py'))
native = importlib.util.module_from_spec(spec)
spec.loader.exec_module(native)


def call(address, path, payload=None, token='', status=200, context=None):
    headers = {'Authorization': 'Bearer ' + token} if token else {}
    if isinstance(payload, dict):
        payload = json.dumps(payload).encode()
        headers['Content-Type'] = 'application/json'
    request = urllib.request.Request(address + path, data=payload, headers=headers)
    try:
        response = urllib.request.urlopen(request, context=context, timeout=5)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        body = response.read()
        assert response.status == status, (path, response.status, status, body)
        return body


def main():
    binary = Path('bin/goteddycloud').resolve()
    with tempfile.TemporaryDirectory(prefix='roles-smoke-', dir='.cache') as temporary:
        root = Path(temporary).resolve() / 'fixture'
        subprocess.run(['go', 'run', '-buildvcs=false', './cmd/fixturegen', '-output', str(root)], check=True)
        context = ssl.create_default_context(cafile=str(root / 'certs/server/teddy-cert.pem'))
        client = root / 'certs/client/001122334455'
        # Exercise the existing name-based identity policy with a synthetic CA.
        for command in [
            ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-subj', '/CN=TeddyCloud test CA', '-keyout', str(root / 'test-ca.key'), '-out', str(root / 'test-ca.pem')],
            ['req', '-new', '-key', str(client / 'teddy-key.pem'), '-subj', '/CN=001122334455', '-out', str(root / 'client.csr')],
            ['x509', '-req', '-in', str(root / 'client.csr'), '-CA', str(root / 'test-ca.pem'), '-CAkey', str(root / 'test-ca.key'), '-set_serial', '2', '-days', '1', '-out', str(root / 'test-client.pem')],
        ]:
            subprocess.run(['openssl', *command], check=True, capture_output=True)
        context.load_cert_chain(root / 'test-client.pem', client / 'teddy-key.pem')
        shared_client = root / 'certs/client/AABBCCDDEEFF'
        for command in [
            ['req', '-new', '-key', str(shared_client / 'teddy-key.pem'), '-subj', '/CN=AABBCCDDEEFF', '-out', str(root / 'shared.csr')],
            ['x509', '-req', '-in', str(root / 'shared.csr'), '-CA', str(root / 'test-ca.pem'), '-CAkey', str(root / 'test-ca.key'), '-set_serial', '3', '-days', '1', '-out', str(root / 'shared-client.pem')],
        ]:
            subprocess.run(['openssl', *command], check=True, capture_output=True)
        shared_context = ssl.create_default_context(cafile=str(root / 'certs/server/teddy-cert.pem'))
        shared_context.load_cert_chain(root / 'shared-client.pem', shared_client / 'teddy-key.pem')
        for restart in range(2):
            process, addresses = native.start(binary, root)
            address = 'http://' + addresses['http']
            try:
                if restart == 0:
                    call(address, '/api/auth/users/create', {'username': 'admin', 'password': 'secret'})
                    call(address, '/api/auth/enabled', {'enabled': True})
                admin = json.loads(call(address, '/api/auth/login', {'username': 'admin', 'password': 'secret'}))['token']
                if restart == 0:
                    call(address, '/api/auth/users/create', {'username': 'alice', 'password': 'secret'}, admin)
                    call(address, '/api/auth/users/create', {'username': 'bob', 'password': 'secret', 'role': 'reader'}, admin)
                    call(address, '/api/auth/users/access', {'username': 'alice', 'boxes': ['001122334455']}, admin)
                alice = json.loads(call(address, '/api/auth/login', {'username': 'alice', 'password': 'secret'}))['token']
                bob = json.loads(call(address, '/api/auth/login', {'username': 'bob', 'password': 'secret'}))['token']
                call(address, '/api/auth/users/details', token=bob, status=403)
                call(address, '/api/fileIndex?area=alice', token=bob, status=403)
                call(address, '/api/fileDelete', b'private.txt', bob, status=403)
                call(address, '/tools/users', token=alice)
                if restart == 0:
                    call(address, '/content/json/set/0000000100000002', b'tonie_model=alice-only&nocloud=true', alice)
                    call(address, '/api/settings/set/toniebox.max_vol_spk?overlay=001122334455', b'2', alice)
                # Catalog grants cross account boundaries without area membership.
                personal = next(area for area in json.loads(call(address, '/api/auth/areas', token=alice))['areas'] if area['name'] == 'alice')
                if restart == 0:
                    area_base = root / 'users' / hashlib.sha256(personal['id'].encode()).hexdigest()
                    library = area_base / 'data/library'
                    library.mkdir(parents=True, exist_ok=True)
                    (library / 'shared.mp3').write_bytes(b'catalog-shared-audio')
                    (library / 'private.mp3').write_bytes(b'catalog-private-audio')
                    call(address, '/api/auth/catalog/grant', {'area': personal['id'], 'username': 'bob', 'all': False, 'items': [{'root': 'library', 'path': 'shared.mp3'}]}, alice)
                catalog_query = '?area=' + personal['id']
                shared_items = json.loads(call(address, '/api/auth/catalog/items' + catalog_query, token=bob))['items']
                assert [item['path'] for item in shared_items] == ['shared.mp3'], shared_items
                assert call(address, '/api/auth/catalog/download' + catalog_query + '&root=library&path=shared.mp3', token=bob) == b'catalog-shared-audio'
                call(address, '/api/auth/catalog/download' + catalog_query + '&root=library&path=private.mp3', token=bob, status=403)
                call(address, '/api/auth/area', {'area': personal['id']}, bob, status=403)
                call(address, '/tools/shares', token=alice)
                if restart == 1:
                    call(address, '/api/auth/catalog/revoke', {'area': personal['id'], 'username': 'bob'}, alice)
                    call(address, '/api/auth/catalog/items' + catalog_query, token=bob, status=403)
                metadata = json.loads(call(address, '/content/json/get/0000000100000002', token=alice))
                assert metadata['tonie_model'] == 'alice-only', metadata
                call(address, '/content/json/get/0000000100000002', token=bob, status=404)
                body = call('https://' + addresses['box'], '/v1/freshness-check', b'', context=context)
                # No marked tags in this fixture: field 3 (speaker volume) precedes field 4.
                assert b'\x18\x02\x20' in body, ('owned device did not use private overlay', body)
                global_volume = call(address, '/api/settings/get/toniebox.max_vol_spk?overlay=001122334455&area=', token=admin)
                # An explicit global selection still follows an owned overlay to its area.
                assert global_volume == b'2', global_volume
                if restart == 0:
                    family = json.loads(call(address, '/api/auth/areas/create', {'name': 'Family', 'members': [{'username': 'alice', 'role': 'admin'}, {'username': 'bob', 'role': 'editor'}], 'boxes': ['AABBCCDDEEFF']}, admin))['area']
                    office = json.loads(call(address, '/api/auth/areas/create', {'name': 'Office', 'members': [{'username': 'alice', 'role': 'reader'}]}, admin))['area']
                    call(address, '/content/json/set/0000000100000003?area=' + family['id'], b'tonie_model=shared-family', bob)
                    call(address, '/api/settings/set/toniebox.max_vol_spk?overlay=AABBCCDDEEFF&area=' + family['id'], b'1', bob)
                    call(address, '/content/json/set/0000000100000003?area=' + office['id'], b'tonie_model=forbidden', alice, status=403)
                    call(address, '/api/auth/area', {'area': office['id']}, bob, status=403)
                    family['members'][1]['role'] = 'reader'
                    call(address, '/api/auth/areas/update', {'area': family['id'], 'members': family['members']}, alice)
                else:
                    family = next(area for area in json.loads(call(address, '/api/auth/areas', token=alice))['areas'] if area['name'] == 'Family')
                call(address, '/api/auth/area', {'area': family['id']}, bob)
                shared = json.loads(call(address, '/content/json/get/0000000100000003', token=bob))
                assert shared['tonie_model'] == 'shared-family', shared
                call(address, '/content/json/set/0000000100000003', b'tonie_model=forbidden', bob, status=403)
                call(address, '/api/auth/users/access', {'username': 'alice', 'role': 'admin'}, alice, status=403)
                call(address, '/api/auth/areas/update', {'area': family['id'], 'name': 'forbidden'}, bob, status=403)
                shared_device = call('https://' + addresses['box'], '/v1/freshness-check', b'', context=shared_context)
                assert b'\x18\x01\x20' in shared_device, ('shared box did not use area settings', shared_device)
                assert any(member['username'] == 'bob' and member['role'] == 'reader' for member in family['members'])
            finally:
                native.stop(process, addresses)
    print('PASS native catalog grants and revocation, private/shared areas, independent member roles, permission revocation, TLS devices, restart persistence and shutdown')


if __name__ == '__main__':
    main()
