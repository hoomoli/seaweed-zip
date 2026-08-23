import importlib.util
import pathlib
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('notices', pathlib.Path(__file__).parents[1] / 'generate_notices.py')
notices = importlib.util.module_from_spec(spec)
spec.loader.exec_module(notices)


class NpmLicenseTest(unittest.TestCase):
    def test_missing_unreviewed_license_fails_with_package_context(self):
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(ValueError, 'Missing npm license: unknown@1.0.0'):
                notices.npm_license_files('unknown', {'version': '1.0.0'}, pathlib.Path(temporary))

    def test_is_mobile_fallback_is_version_locked(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = pathlib.Path(temporary)
            files = notices.npm_license_files('is-mobile', {'version': '5.0.0'}, directory)
            self.assertEqual([notices.ROOT / 'docs/licenses/is-mobile.txt'], files)
            with self.assertRaisesRegex(ValueError, 'License fallback version drift: is-mobile'):
                notices.npm_license_files('is-mobile', {'version': '6.0.0'}, directory)

    def test_shipped_license_takes_precedence(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = pathlib.Path(temporary)
            license = directory / 'LICENSE'
            license.write_text('Reviewed upstream text')
            self.assertEqual([license], notices.npm_license_files('is-mobile', {'version': '6.0.0'}, directory))

    def test_rolldown_notice_includes_third_party_text(self):
        with tempfile.TemporaryDirectory() as temporary, patch.object(notices, 'ROOT', pathlib.Path(temporary)):
            for name in ('vite', 'rolldown'):
                directory = pathlib.Path(temporary) / 'web/node_modules' / name
                directory.mkdir(parents=True)
                (directory / 'package.json').write_text('{"version":"1.0.0"}')
                (directory / 'LICENSE').write_text('Main grant')
            (directory / 'THIRD-PARTY-LICENSE').write_text('Bundled third-party grant')
            output = notices.build_helper_notices()
            self.assertIn('Build runtime helpers: rolldown@1.0.0 / THIRD-PARTY-LICENSE', output)
            self.assertIn('Bundled third-party grant', output)
            self.assertNotIn('Build runtime helpers: rollup', output)
