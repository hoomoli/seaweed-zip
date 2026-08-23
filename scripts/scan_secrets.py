#!/usr/bin/env python3
"""Scan the complete public snapshot and all reachable history, never log secrets."""
import os
import pathlib
import shutil
import subprocess
import tempfile
from repository_policy import candidates

IMAGE = 'ghcr.io/gitleaks/gitleaks:v8.30.1@sha256:c00b6bd0aeb3071cbcb79009cb16a60dd9e0a7c60e2be9ab65d25e6bc8abbb7f'
ROOT = pathlib.Path(__file__).resolve().parents[1]


def run_scanner(command, phase, findings_code=None):
    try:
        result = subprocess.run(command, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    except OSError:
        raise SystemExit(f'Secret scan {phase}: Docker could not be started; check installation and permissions.') from None
    if result.returncode:
        category = 'potential secrets detected' if result.returncode == findings_code else 'scanner runtime failure'
        raise SystemExit(f'Secret scan {phase}: {category} (exit {result.returncode}); output suppressed. Inspect privately with redaction.')


def pull_image():
    run_scanner(['docker', 'pull', IMAGE], 'image pull')


def scan(path, history=False):
    # Match the owner of the private 0700 snapshot without restoring DAC capabilities.
    command = ['docker', 'run', '--rm', '--user', f'{os.getuid()}:{os.getgid()}', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges', '--tmpfs', '/tmp:rw,noexec,nosuid,nodev,size=64m', '-v', f'{path}:/repo:ro', IMAGE, 'detect', '--source=/repo', '--redact=100', '--no-banner', '--exit-code=10']
    command += ['--log-opts=--all'] if history else ['--no-git']
    run_scanner(command, 'history' if history else 'snapshot', findings_code=10)


if __name__ == '__main__':
    pull_image()
    with tempfile.TemporaryDirectory(prefix='seaweed-zip-public-') as temporary:
        for name in candidates():
            source = ROOT / name
            if source.is_symlink():
                raise SystemExit('Symlink candidate rejected; run repository policy.')
            if source.is_file():
                target = pathlib.Path(temporary) / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(source, target)
        scan(temporary)
    if subprocess.run(['git', 'rev-parse', '--verify', 'HEAD'], cwd=ROOT, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
        scan(ROOT, history=True)
        print('Public snapshot and all reachable Git history secret scans passed.')
    else:
        print('Public snapshot secret scan passed; unborn repository has no commit history.')
