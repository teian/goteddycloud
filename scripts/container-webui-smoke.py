#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Verify bundled UI delivery with disposable volume and host-mount deployments."""
import argparse
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


def docker(*args, **kwargs):
    return subprocess.check_output(['docker', *args], **kwargs)


class Assets(HTMLParser):
    def __init__(self):
        super().__init__()
        self.base = None
        self.urls = []

    def handle_starttag(self, tag, attributes):
        attributes = dict(attributes)
        if tag == 'base' and self.base is None:
            self.base = attributes['href']
        if tag == 'script' and 'src' in attributes:
            self.urls.append(attributes['src'])
        if tag == 'link' and attributes.get('rel') in ('stylesheet', 'modulepreload', 'icon'):
            self.urls.append(attributes['href'])


def check_ui(origin):
    for route in ('/web', '/web/', '/web/settings/users', '/web/tonieboxes/plugin/goteddycloud-cc3200'):
        with urllib.request.urlopen(origin + route, timeout=5) as response:
            assert response.status == 200 and response.headers.get_content_type() == 'text/html'
            page = Assets()
            page.feed(response.read().decode())
        assert page.base is not None and page.urls, 'UI entry has no base or assets'
        base = urllib.parse.urljoin(origin + route, page.base)
        assert base == origin + '/web/', (route, base)
        for asset in page.urls:
            url = urllib.parse.urljoin(base, asset)
            if urllib.parse.urlsplit(url).netloc != urllib.parse.urlsplit(origin).netloc:
                continue
            with urllib.request.urlopen(url, timeout=5) as response:
                assert response.status == 200 and response.read(), url
                assert response.headers.get_content_type() != 'text/html', url
    try:
        urllib.request.urlopen(origin + '/web/nonexistent-asset.js', timeout=5)
    except urllib.error.HTTPError as error:
        assert error.code == 404
    else:
        raise AssertionError('missing asset returned SPA HTML')


def ready(name):
    state = json.loads(docker('inspect', name, text=True))[0]
    origin = 'http://127.0.0.1:' + state['NetworkSettings']['Ports']['18080/tcp'][0]['HostPort']
    for _ in range(100):
        try:
            with urllib.request.urlopen(origin + '/api/time', timeout=1):
                return origin
        except OSError:
            time.sleep(0.1)
    raise AssertionError(docker('logs', name, text=True, stderr=subprocess.STDOUT))


def run(image, mounts):
    name = 'goteddycloud-webui-' + uuid.uuid4().hex[:12]
    try:
        docker('run', '-d', '--name', name, '--read-only', '--tmpfs', '/tmp',
               '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true',
               '-p', '127.0.0.1::18080', *mounts, image,
               '-base_path', '/data', '-http', ':18080', '-httpsecure', 'off', '-box', 'off')
        origin = ready(name)
        check_ui(origin)
        # Bundled assets must not have been copied into persistent data.
        docker('exec', name, 'test', '!', '-e', '/data/data/www/web/index.html')
        docker('exec', name, 'mkdir', '-p', '/data/data/www/web')
        docker('exec', '-i', name, 'tee', '/data/data/www/web/index.html', input=b'custom UI')
        with urllib.request.urlopen(origin + '/web/settings/users', timeout=5) as response:
            assert response.read() == b'custom UI', 'custom frontend did not override bundle'
        docker('exec', name, 'rm', '/data/data/www/web/index.html')
        check_ui(origin)
        docker('restart', '-t', '5', name)
        origin = ready(name)
        check_ui(origin)
        docker('stop', '-t', '5', name)
        assert docker('wait', name, text=True).strip() == '0'
    finally:
        subprocess.run(['docker', 'rm', '-fv', name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--image', default='goteddycloud:local')
    args = parser.parse_args()
    run(args.image, [])  # Default UID/GID 65532, writable anonymous /data volume.
    with tempfile.TemporaryDirectory(prefix='goteddycloud-webui-') as directory:
        run(args.image, ['--user', f'{os.getuid()}:{os.getgid()}', '-v', f'{Path(directory).resolve()}:/data:Z'])
    print('PASS bundled Web UI, deep links/assets, custom override, empty host mount, read-only image and restart')


if __name__ == '__main__':
    main()
