import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch


SCRIPT = Path(__file__).resolve().parents[2] / "scripts/fusion/private-glm-key.py"
FAKE_KEY = "fixture-only.glm-key-value"


class PrivateGLMKeyTests(unittest.TestCase):
    def setUp(self):
        self.assertTrue(SCRIPT.is_file(), "private key entry utility is missing")
        spec = importlib.util.spec_from_file_location("private_glm_key", SCRIPT)
        self.tool = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.tool)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "fusion-gateway"

    def test_store_private_readable_key(self):
        path = self.tool.store_key(self.root, FAKE_KEY)
        self.assertEqual(self.tool.read_key(self.root), FAKE_KEY)
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(self.root.stat().st_mode), 0o700)
        self.assertEqual(stat.S_IMODE(path.parent.stat().st_mode), 0o700)

    def test_refuse_overwriting_existing_key(self):
        path = self.tool.store_key(self.root, FAKE_KEY)
        with self.assertRaises(self.tool.KeyError):
            self.tool.store_key(self.root, "another.fixture-key")
        self.assertEqual(path.read_text().strip(), FAKE_KEY)

    def test_refuse_symlink_root(self):
        target = Path(self.temp.name) / "target"
        target.mkdir()
        self.root.symlink_to(target, target_is_directory=True)
        with self.assertRaises(self.tool.KeyError):
            self.tool.store_key(self.root, FAKE_KEY)
        self.assertEqual(list(target.iterdir()), [])

    def test_refuse_public_directory(self):
        self.root.mkdir(mode=0o755)
        with self.assertRaises(self.tool.KeyError):
            self.tool.store_key(self.root, FAKE_KEY)
        self.assertEqual(list(self.root.iterdir()), [])

    def test_refuse_key_in_git_checkout(self):
        project = Path(self.temp.name) / "project"
        project.mkdir()
        (project / ".git").write_text("gitdir: fixture")
        with self.assertRaises(self.tool.KeyError):
            self.tool.store_key(project / "private", FAKE_KEY)
        self.assertFalse((project / "private").exists())

    def test_parent_symlink_cannot_hide_git_checkout(self):
        project = Path(self.temp.name) / "project"
        project.mkdir()
        (project / ".git").mkdir()
        alias = Path(self.temp.name) / "alias"
        alias.symlink_to(project, target_is_directory=True)
        with self.assertRaises(self.tool.KeyError):
            self.tool.store_key(alias / "private", FAKE_KEY)
        self.assertFalse((project / "private").exists())

    def test_dangling_git_marker_does_not_allow_credentials(self):
        project = Path(self.temp.name) / "project"
        project.mkdir()
        (project / ".git").symlink_to("missing-gitdir")
        with self.assertRaises(self.tool.KeyError):
            self.tool.store_key(project / "private", FAKE_KEY)
        self.assertFalse((project / "private").exists())

    def test_refuse_public_key_file_and_symlink(self):
        path = self.tool.store_key(self.root, FAKE_KEY)
        path.chmod(0o644)
        with self.assertRaises(self.tool.KeyError):
            self.tool.read_key(self.root)
        path.unlink()
        target = Path(self.temp.name) / "fixture-token"
        target.write_text(FAKE_KEY)
        target.chmod(0o600)
        path.symlink_to(target)
        with self.assertRaises(self.tool.KeyError):
            self.tool.read_key(self.root)

    def test_invalid_key_is_not_persisted_or_in_error(self):
        for key in ["", "REPLACE_WITH_LOCAL_GLM_CODING_PLAN_KEY", "secret\nheader", "secret value", "a" * 8193]:
            with self.subTest(length=len(key)):
                with self.assertRaises(self.tool.KeyError) as failure:
                    self.tool.store_key(self.root, key)
                if key:
                    self.assertNotIn(key, str(failure.exception))
                self.assertFalse((self.root / "credentials/glm-coding-plan.key").exists())

    def test_missing_status_does_not_create_directories(self):
        result = self.tool.status(self.root)
        self.assertFalse(result["configured"])
        self.assertFalse(self.root.exists())

    def test_hidden_entry_prints_only_path_and_status(self):
        output = io.StringIO()
        with patch.object(self.tool.getpass, "getpass", return_value=FAKE_KEY), patch("sys.stdout", output):
            self.assertEqual(self.tool.main(["init", "--root", str(self.root)]), 0)
            self.assertEqual(self.tool.main(["status", "--root", str(self.root)]), 0)
        self.assertNotIn(FAKE_KEY, output.getvalue())
        self.assertIn("configured", output.getvalue())


if __name__ == "__main__":
    unittest.main()
