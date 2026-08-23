#!/usr/bin/env python3
"""Check publication candidates without printing potentially sensitive contents."""
import argparse
import ipaddress
import pathlib
import re
import subprocess
import sys


def git(*args):
    return subprocess.check_output(['git', *args])


def candidates():
    return sorted(set(p for p in git('ls-files', '-z', '--cached', '--others', '--exclude-standard').decode().split('\0') if p))


def path_problem(path):
    if not path.isascii():
        return 'non-ASCII path'
    parts = pathlib.PurePosixPath(path).parts
    forbidden = {'.agents', '.agent-work', '.codex', '.superpowers', '.logic-lines', 'node_modules', 'dist', 'data', 'coverage', 'test-results', 'playwright-report', 'blob-report', 'screenshots', 'artifacts', '.git'}
    if any(p in forbidden for p in parts) or path.startswith('docs/superpowers/'):
        return 'private or generated path'
    name = parts[-1].lower()
    if name in {'agent.md', 'agents.md', 'claude.md', 'gemini.md', 'handoff.md', '.npmrc', '.netrc', '.ds_store', 'seaweed-zip'}:
        return 'private filename'
    if name.startswith('.env') and name != '.env.example':
        return 'environment file'
    if re.search(r'\.(db|sqlite3?|log|key|pem|p12|pfx|jks|keystore|credentials|secret|bak|backup|zip|tar|tgz|7z|png|jpe?g|webp|mp4|test|tsbuildinfo)(-|$)', name):
        return 'runtime, sensitive or generated file'
    return None


def content_problems(path, text):
    problems = []
    if path != 'web/src/locales/zh-CN.ts' and re.search('[\u4e00-\u9fff]', text):
        problems.append('localization')
    if re.search(r'(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{40,}|AKIA[0-9A-Z]{16}|-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----)', text):
        problems.append('credential')
    for match in re.findall(r'(?<![\d.])(?:\d{1,3}\.){3}\d{1,3}(?![\d.])', text):
        try:
            addr = ipaddress.ip_address(match)
            if path not in {'internal/httpapi/api_test.go', 'scripts/repository_policy.py', 'scripts/tests/test_policy.py'} and (addr in ipaddress.ip_network('10.0.0.0/8') or addr in ipaddress.ip_network('172.16.0.0/12') or addr in ipaddress.ip_network('192.168.0.0/16')):
                problems.append('private address')
                break
        except ValueError:
            pass
    if path != 'scripts/repository_policy.py' and re.search(r'/Users/[^/\s]+/|host-35|\bLQSDK\b', text):
        problems.append('internal context')
    if path.startswith('.github/workflows/'):
        if re.search(r'uses:\s*[^\s]+@(?![0-9a-f]{40}(?:\s|$))', text):
            problems.append('unpinned action')
    if path == 'Dockerfile':
        for line in text.splitlines():
            if line.startswith('FROM ') and line.split()[1] != 'scratch' and not re.search(r'@sha256:[0-9a-f]{64}(?:\s|$)', line):
                problems.append('unpinned base image')
    return problems


def image_input(path):
    if path.endswith('_test.go') or re.search(r'\.(test|spec)\.[jt]sx?$', path) or '/e2e/' in path:
        return False
    return (path in {'Dockerfile', '.dockerignore', 'go.mod', 'go.sum', 'LICENSE', 'THIRD_PARTY_NOTICES.txt'}
            or path.startswith(('cmd/', 'internal/', 'web/src/'))
            or path in {'web/embed.go', 'web/index.html', 'web/package.json', 'web/package-lock.json', 'web/tsconfig.json', 'web/vite.config.ts', 'scripts/generate_notices.py'})


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--image-changed', nargs=2, metavar=('BEFORE', 'AFTER'))
    args = parser.parse_args()
    if args.image_changed:
        before, after = args.image_changed
        paths = candidates() if set(before) == {'0'} else git('diff', '--name-only', '-z', before, after).decode().split('\0')
        print('true' if any(image_input(p) for p in paths) else 'false')
        return 0
    errors = []
    for path in candidates():
        problem = path_problem(path)
        if problem:
            errors.append((path, problem))
            continue
        file = pathlib.Path(path)
        if file.is_symlink():
            errors.append((path, 'symlinks are not publication candidates'))
            continue
        if not file.is_file():
            continue
        try:
            text = file.read_text(encoding='utf-8')
        except UnicodeDecodeError:
            errors.append((path, 'binary publication candidate'))
            continue
        errors.extend((path, p) for p in content_problems(path, text))
    for path, problem in errors:
        print(f'{path}: {problem}', file=sys.stderr)
    print(f'Repository policy: {len(errors)} violations; contents are redacted.')
    return bool(errors)


if __name__ == '__main__':
    sys.exit(main())
