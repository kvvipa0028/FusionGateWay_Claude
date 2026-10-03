import importlib.util
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch


SCRIPT = Path(__file__).resolve().parents[2] / "scripts/fusion/run-dev.py"


class DevLauncherTests(unittest.TestCase):
    def setUp(self):
        self.assertTrue(SCRIPT.is_file(), "isolated development launcher missing")
        spec = importlib.util.spec_from_file_location("fusion_dev_launcher", SCRIPT)
        self.tool = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.tool)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "fusion-state"

    def test_no_daily_auth_or_provider_environment_is_inherited(self):
        with patch.dict(os.environ, {"ANTHROPIC_AUTH_TOKEN":"must-not-inherit", "CODEX_HOME":"daily-codex", "MAGPIE_ADDR":"0.0.0.0:3425", "FUSION_ENABLED":"1"}):
            env = self.tool.environment(self.root)
        self.assertNotIn("ANTHROPIC_AUTH_TOKEN", env)
        self.assertNotIn("CODEX_HOME", env)
        self.assertNotIn("MAGPIE_ADDR", env)
        self.assertNotIn("FUSION_ENABLED", env)
        self.assertEqual(env["HOME"], str(self.root / "runtime-home"))
        self.assertEqual(env["FUSION_STATE_ROOT"], str(self.root))

    def test_state_and_client_home_are_private_and_separate(self):
        env = self.tool.environment(self.root)
        for field in ("HOME","XDG_CONFIG_HOME","XDG_CACHE_HOME","XDG_DATA_HOME","TMPDIR"):
            path = Path(env[field])
            self.assertTrue(path.is_dir())
            self.assertEqual(stat.S_IMODE(path.stat().st_mode),0o700)
        self.assertEqual(stat.S_IMODE((Path(env["XDG_CONFIG_HOME"])/"fusion-gateway").stat().st_mode),0o700)
        self.assertFalse((self.root / "credentials/glm-coding-plan.key").exists())

    def test_unsafe_state_is_refused_without_chmod_or_redirect(self):
        self.root.mkdir(mode=0o755)
        with self.assertRaises(self.tool.credentials.KeyError):
            self.tool.environment(self.root)
        self.assertEqual(stat.S_IMODE(self.root.stat().st_mode),0o755)
        self.assertEqual(list(self.root.iterdir()),[])

    def test_existing_daily_files_are_not_changed(self):
        daily = Path(self.temp.name)/"daily-home"
        (daily/".claude").mkdir(parents=True)
        settings = daily/".claude/settings.json"
        settings.write_text('{"model":"daily-model"}')
        with patch.dict(os.environ,{"HOME":str(daily)}):
            self.tool.environment(self.root)
        self.assertEqual(settings.read_text(),'{"model":"daily-model"}')

    def fake_go(self, tags):
        file=Path(self.temp.name)/"go-fixture"
        file.write_text("#!/usr/bin/python3\nprint("+repr("fixture: go1.26.3\n\tbuild\t-tags="+tags)+")\n")
        file.chmod(0o700)
        return str(file)

    def test_only_explicit_fusion_build_is_admitted(self):
        self.assertTrue(self.tool.fusion_binary("fixture",self.fake_go("fusion,nogui")))
        self.assertFalse(self.tool.fusion_binary("fixture",self.fake_go("nogui")))
        self.assertFalse(self.tool.fusion_binary("fixture",self.fake_go("notfusion,nogui")))


if __name__ == "__main__":
    unittest.main()
