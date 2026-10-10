#!/usr/bin/env python3
"""Check plugin parity plus the explicitly added built-in Go flashing tool."""
import http.client
import json


def get(port, path):
    connection = http.client.HTTPConnection('127.0.0.1', port, timeout=10)
    try:
        connection.request('GET', path)
        response = connection.getresponse()
        assert response.status == 200, (path, response.status)
        return response.getheader('Content-Type'), response.read()
    finally:
        connection.close()


reference_type, reference = get(18080, '/api/plugins/get')
candidate_type, candidate = get(18081, '/api/plugins/get')
assert reference_type == candidate_type
reference, candidate = json.loads(reference), json.loads(candidate)
assert candidate == ['goteddycloud-cc3200', *reference], (reference, candidate)
assert 'goteddycloud-users' not in candidate
for name in reference:
    assert get(18080, f'/plugins/{name}/plugin.json') == get(18081, f'/plugins/{name}/plugin.json')
_, metadata = get(18081, '/plugins/goteddycloud-cc3200/plugin.json')
assert json.loads(metadata)['teddyCloudSection'] == 'tonieboxes'
kind, preview = get(18081, '/plugins/goteddycloud-cc3200/preview.png')
assert kind == 'image/png' and preview.startswith(b'\x89PNG\r\n\x1a\n')
print('PASS plugin parity, built-in CC3200 metadata/preview, and native user management')
