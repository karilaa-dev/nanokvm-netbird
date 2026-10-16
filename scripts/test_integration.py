#!/usr/bin/env python3
"""Exercise apply, repeat install, and rollback against the captured 0.2.4 bundle."""
import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('integration', Path(__file__).with_name('integrate.py'))
integration = importlib.util.module_from_spec(spec)
spec.loader.exec_module(integration)
PROJECT = Path(__file__).resolve().parent.parent


class IntegrationTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.web, self.backup, self.config = integration.paths(self.root)
        self.web.mkdir(parents=True)
        self.config.parent.mkdir(parents=True)
        self.original = (PROJECT / '.cache/device/assets' / integration.ASSET).read_bytes()
        (self.web / integration.ASSET).write_bytes(self.original)
        self.config.write_text('proto: https\nport:\n    http: 80\n    https: 443\njwt:\n    secretKey: original-test-value\n')
        self.config.chmod(0o600)
        self.module = PROJECT / 'web/netbird-provider.js'

    def test_apply_repeat_restore_preserves_other_settings(self):
        self.assertEqual(integration.check(self.root, self.module), 'ready')
        integration.apply(self.root, self.module)
        patched = (self.web / integration.ASSET).read_bytes()
        self.assertEqual(patched.count(b'cI(__nkNetBird(d));'), 1)
        self.assertIn('https: 8443', self.config.read_text())
        self.assertEqual(self.config.stat().st_mode & 0o777, 0o600)
        self.assertEqual(integration.check(self.root, self.module), 'installed')
        integration.apply(self.root, self.module)
        self.assertEqual((self.web / integration.ASSET).read_bytes(), patched)
        self.config.write_text(self.config.read_text().replace('original-test-value', 'changed-test-value'))
        integration.restore(self.root)
        self.assertEqual((self.web / integration.ASSET).read_bytes(), self.original)
        self.assertIn('https: 443', self.config.read_text())
        self.assertIn('secretKey: changed-test-value', self.config.read_text())
        self.assertFalse((self.web / integration.MODULE).exists())
        integration.restore(self.root)

    def test_unknown_bundle_rejected_before_mutation(self):
        (self.web / integration.ASSET).write_bytes(b'other firmware')
        with self.assertRaisesRegex(ValueError, 'Unsupported'):
            integration.apply(self.root, self.module)
        self.assertIn('https: 443', self.config.read_text())
        self.assertFalse(self.backup.exists())

    def test_rollback_after_interrupted_config_write(self):
        before = self.config.read_bytes()
        integration.apply(self.root, self.module)
        self.config.write_bytes(before)
        integration.restore(self.root)
        self.assertEqual((self.web / integration.ASSET).read_bytes(), self.original)
        self.assertEqual(self.config.read_bytes(), before)


if __name__ == '__main__':
    unittest.main()
