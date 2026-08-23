#!/usr/bin/env python3
"""Run an isolated, non-root, read-only container and test default deny."""
import json
import subprocess
import sys
import time
import urllib.error
import urllib.request

image = sys.argv[1]
container = subprocess.check_output(['docker', 'run', '-d', '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges', '--mount', 'type=volume,destination=/data', '-p', '127.0.0.1::8080', image]).decode().strip()
try:
    info = json.loads(subprocess.check_output(['docker', 'inspect', container]))[0]
    assert info['Config']['User'] == '10001:10001'
    port = info['NetworkSettings']['Ports']['8080/tcp'][0]['HostPort']
    base = f'http://127.0.0.1:{port}'
    for attempt in range(60):
        try:
            with urllib.request.urlopen(base + '/readyz', timeout=2) as response:
                assert response.status == 200
            break
        except (OSError, urllib.error.URLError):
            time.sleep(1)
    else:
        raise AssertionError('readiness timeout')
    subprocess.run(['docker', 'exec', container, '/usr/local/bin/seaweed-zip', 'healthcheck'], check=True)
    for path in ('/', '/api/v1/admin/packs'):
        try:
            urllib.request.urlopen(base + path, timeout=2)
            raise AssertionError('expected default-deny 403')
        except urllib.error.HTTPError as error:
            assert error.code == 403
    print('Image smoke passed: UID 10001, read-only root, readiness, health and management default deny.')
finally:
    subprocess.run(['docker', 'rm', '-fv', container], check=True, stdout=subprocess.DEVNULL)
