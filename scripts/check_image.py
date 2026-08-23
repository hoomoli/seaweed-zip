#!/usr/bin/env python3
"""Gate scan results without ever printing secret match data."""
import collections
import json
import sys

report = json.load(open(sys.argv[1]))
counts = collections.Counter()
blocked = 0
for result in report.get('Results', []):
    for secret in result.get('Secrets', []):
        counts['secrets'] += 1
        blocked += 1
    for finding in result.get('Vulnerabilities', []):
        severity = finding.get('Severity', 'UNKNOWN')
        fixed = bool(finding.get('FixedVersion'))
        counts[f'{severity}: {"fix available" if fixed else "no fix reported"}'] += 1
        blocked += int(severity in {'HIGH', 'CRITICAL'} and fixed)
    for finding in result.get('Misconfigurations', []):
        if finding.get('Status') == 'FAIL':
            counts['configuration failures'] += 1
            blocked += int(finding.get('Severity') in {'HIGH', 'CRITICAL'})
print('Image scan counts (unfixed findings included):', dict(sorted(counts.items())))
if blocked:
    sys.exit('Image publication blocked by scan policy; matches are not logged.')
