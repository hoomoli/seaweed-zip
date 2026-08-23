import importlib.util
import pathlib
import os
import subprocess
import tempfile
import textwrap
import unittest

spec = importlib.util.spec_from_file_location('policy', pathlib.Path(__file__).parents[1] / 'repository_policy.py')
policy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(policy)


class PolicyTest(unittest.TestCase):
    def test_image_inputs(self):
        for path in ('cmd/seaweed-zip/main.go', 'internal/store/store.go', 'web/src/main.tsx', 'web/package-lock.json', 'Dockerfile', 'LICENSE', 'THIRD_PARTY_NOTICES.txt'):
            self.assertTrue(policy.image_input(path), path)
        for path in ('README.md', 'docs/operations.md', 'internal/store/store_test.go', 'web/e2e/ui.spec.ts', 'web/src/model.test.ts', 'scripts/tests/test_policy.py'):
            self.assertFalse(policy.image_input(path), path)

    def test_forbidden_names(self):
        for path in ('.env.prod', 'data/state.sqlite', '.agents/rules.md', 'AGENT.md', 'web/dist/index.html', 'capture.png', 'keys/server.pem', 'secret.credentials'):
            self.assertTrue(policy.path_problem(path), path)
        for path in ('.github/workflows/ci.yml', 'web/src/main.tsx', 'api/openapi.yaml', 'compose.yaml'):
            self.assertIsNone(policy.path_problem(path), path)

    def test_content_redaction_and_localization(self):
        secret = 'ghp_' + 'a' * 36
        self.assertIn('credential', policy.content_problems('README.md', secret))
        self.assertNotIn(secret, repr(policy.content_problems('README.md', secret)))
        self.assertIn('private address', policy.content_problems('README.md', '.'.join(['10','23','45','67'])))
        self.assertEqual([], policy.content_problems('README.md', '127.0.0.1 192.0.2.1 storage.example.invalid'))
        text = chr(0x4e2d)
        self.assertIn('localization', policy.content_problems('web/src/main.tsx', text))
        self.assertNotIn('localization', policy.content_problems('web/src/locales/zh-CN.ts', text))


if __name__ == '__main__':
    unittest.main()

class ContainerContractTest(unittest.TestCase):
    def test_writable_data_and_ownership_with_readonly_root(self):
        root = pathlib.Path(__file__).parents[2]
        self.assertIn('--chown=10001:10001 /rootfs/data /data', (root / 'Dockerfile').read_text())
        script = (root / 'scripts/smoke_image.py').read_text()
        self.assertIn('type=volume,destination=/data', script)
        self.assertIn('--read-only', script)

class WorkflowContractTest(unittest.TestCase):
    def test_validation_caches_only_reusable_dependencies_and_builds(self):
        workflow = (pathlib.Path(__file__).parents[2] / '.github/workflows/ci.yml').read_text()
        go_setup = workflow.split('      - uses: actions/setup-go@', 1)[1].split('      - uses:', 1)[0]
        self.assertIn('cache: true', go_setup)
        self.assertIn('cache-dependency-path: |\n            go.sum\n            .github/workflows/ci.yml', go_setup)
        node_setup = workflow.split('      - uses: actions/setup-node@', 1)[1].split('      - name:', 1)[0]
        self.assertIn('cache: npm', node_setup)
        self.assertIn('cache-dependency-path: web/package-lock.json', node_setup)
        browser_cache = workflow.split('      - name: Cache Playwright browsers\n', 1)[1].split('      - name:', 1)[0]
        self.assertIn('path: ~/.cache/ms-playwright', browser_cache)
        self.assertIn("key: playwright-v1-${{ runner.os }}-${{ runner.arch }}-${{ hashFiles('web/package-lock.json') }}", browser_cache)
        self.assertNotIn('restore-keys:', browser_cache)
        self.assertNotIn('cache-hit', workflow)
        for command in ('npm ci --prefix web --ignore-scripts', 'npm audit --prefix web', 'go test -race -count=1 ./...', 'go vet ./...', 'go install golang.org/x/vuln/cmd/govulncheck@v1.9.0', 'npx playwright install --with-deps chromium', 'npm run test:e2e'):
            self.assertIn(command, workflow)

    def test_manual_publish_entry_and_main_only_job_gates(self):
        workflow = (pathlib.Path(__file__).parents[2] / '.github/workflows/ci.yml').read_text()
        self.assertIn('  workflow_dispatch:', workflow)
        condition = "if: (github.event_name == 'push' || github.event_name == 'workflow_dispatch') && github.ref == 'refs/heads/main' && needs.validate.outputs.image_changed == 'true'"
        self.assertEqual(2, workflow.count(condition))
        self.assertIn('needs: validate', workflow)
        self.assertIn('needs: [validate, image]', workflow)

    def test_manual_publish_bypasses_diff_but_automatic_push_keeps_it(self):
        root = pathlib.Path(__file__).parents[2]
        workflow = (root / '.github/workflows/ci.yml').read_text()
        block = workflow.split('      - name: Determine production inputs\n', 1)[1].split('      - name:', 1)[0]
        script = textwrap.dedent(block.split('        run: |\n', 1)[1])
        for event, before, expected in (('workflow_dispatch', 'invalid-ref', 'true'), ('pull_request', 'invalid-ref', 'false'), ('push', 'HEAD', 'false'), ('push', '0' * 40, 'true')):
            with self.subTest(event=event, before=before), tempfile.TemporaryDirectory() as temporary:
                output = pathlib.Path(temporary) / 'output'
                env = dict(os.environ, EVENT=event, BEFORE=before, AFTER='HEAD', GITHUB_OUTPUT=str(output))
                subprocess.run(['bash', '-e', '-c', script], cwd=root, env=env, check=True, capture_output=True)
                self.assertEqual(f'changed={expected}\n', output.read_text())

    def test_no_implicit_build_record_upload(self):
        workflow = (pathlib.Path(__file__).parents[2] / '.github/workflows/ci.yml').read_text()
        self.assertIn('DOCKER_BUILD_RECORD_UPLOAD: false', workflow)

    def test_vulnerability_audit_covers_both_linux_distribution_targets(self):
        workflow = (pathlib.Path(__file__).parents[2] / '.github/workflows/ci.yml').read_text()
        self.assertIn('go install golang.org/x/vuln/cmd/govulncheck@v1.9.0', workflow)
        self.assertNotIn('govulncheck@v1.1.4', workflow)
        self.assertIn('for arch in amd64 arm64; do', workflow)
        self.assertIn('GOOS=linux GOARCH="$arch" "$GOBIN/govulncheck" ./...', workflow)

class NoticeGraphTest(unittest.TestCase):
    def test_distribution_graph_is_target_independent(self):
        from unittest.mock import patch
        spec = importlib.util.spec_from_file_location('notices', pathlib.Path(__file__).parents[1] / 'generate_notices.py')
        notices = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(notices)
        def mock_run(*args, **kwargs):
            arch = kwargs['env']['GOARCH']
            self.assertEqual('linux', kwargs['env']['GOOS'])
            return '{"Module":{"Path":"' + arch + '","Version":"v1","Dir":"/tmp"}}'
        with patch.object(notices, 'run', side_effect=mock_run):
            self.assertEqual({'amd64','arm64'}, set(notices.runtime_modules()))

    def test_bundled_build_helpers_have_notices(self):
        text = (pathlib.Path(__file__).parents[2] / 'THIRD_PARTY_NOTICES.txt').read_text()
        self.assertTrue('Build runtime helpers: vite' in text)
        self.assertTrue('Build runtime helpers: rollup' in text)

class ConfigurationGateTest(unittest.TestCase):
    def test_configuration_scan_precedes_image_upload(self):
        workflow = (pathlib.Path(__file__).parents[2] / '.github/workflows/ci.yml').read_text()
        self.assertIn('scan-type: config', workflow)
        self.assertIn('python3 scripts/check_image.py /tmp/seaweed-zip-config.json', workflow)
