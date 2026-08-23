import os
import pathlib
import subprocess
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(pathlib.Path(__file__).parents[1]))
import scan_secrets


class SecretScannerTest(unittest.TestCase):
    def test_scans_as_snapshot_owner_without_relaxing_sandbox(self):
        for history in (False, True):
            with self.subTest(history=history), patch.object(os, 'getuid', return_value=1001), patch.object(os, 'getgid', return_value=1002), patch.object(scan_secrets.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0)) as run:
                scan_secrets.scan('/fixture', history=history)
                command = run.call_args.args[0]
                self.assertEqual(subprocess.DEVNULL, run.call_args.kwargs['stdout'])
                self.assertEqual(subprocess.DEVNULL, run.call_args.kwargs['stderr'])
                self.assertEqual('1001:1002', command[command.index('--user') + 1])
                self.assertEqual('ALL', command[command.index('--cap-drop') + 1])
                self.assertEqual('none', command[command.index('--network') + 1])
                self.assertIn('--read-only', command)
                self.assertIn('--redact=100', command)
                self.assertIn('--exit-code=10', command)
                self.assertIn('--log-opts=--all' if history else '--no-git', command)

    def test_failures_identify_phase_and_category_without_output(self):
        for history in (False, True):
            for code, category in ((10, 'potential secrets detected'), (1, 'scanner runtime failure'), (125, 'scanner runtime failure')):
                with self.subTest(history=history, code=code), patch.object(scan_secrets.subprocess, 'run', return_value=subprocess.CompletedProcess([], code, stderr='sensitive output')):
                    with self.assertRaises(SystemExit) as raised:
                        scan_secrets.scan('/private/fixture', history=history)
                    message = str(raised.exception)
                    self.assertIn('history' if history else 'snapshot', message)
                    self.assertIn(category, message)
                    self.assertIn(f'exit {code}', message)
                    self.assertNotIn('sensitive output', message)
                    self.assertNotIn('/private/fixture', message)

    def test_docker_unavailable_is_safe_and_phase_specific(self):
        with patch.object(scan_secrets.subprocess, 'run', side_effect=FileNotFoundError('sensitive path')):
            with self.assertRaisesRegex(SystemExit, 'snapshot: Docker could not be started'):
                scan_secrets.scan('/fixture')

    def test_image_pull_failure_is_sanitized(self):
        with patch.object(scan_secrets.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, stderr='sensitive output')):
            with self.assertRaisesRegex(SystemExit, 'image pull: scanner runtime failure .*exit 1'):
                scan_secrets.pull_image()
