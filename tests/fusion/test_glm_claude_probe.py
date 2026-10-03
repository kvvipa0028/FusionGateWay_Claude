import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


SCRIPT = Path(__file__).resolve().parents[2] / "scripts/fusion/glm-claude-probe.py"
FAKE_KEY = "fixture-only.private-token"
MODEL = "glm-5.3"


class GLMClaudeProbeTests(unittest.TestCase):
    def setUp(self):
        self.assertTrue(SCRIPT.is_file(), "Claude GLM diagnostic launcher is missing")
        spec = importlib.util.spec_from_file_location("glm_claude_probe", SCRIPT)
        self.tool = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.tool)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "private"
        self.tool.credentials.store_key(self.root, FAKE_KEY)

    def fake_cli(self, source):
        cli = Path(self.temp.name) / "claude-fixture"
        cli.write_text("#!/usr/bin/python3\n" + source)
        cli.chmod(0o700)
        return str(cli)

    def success_cli(self, extra=""):
        return self.fake_cli(
            "import json,os,sys\n"
            "assert os.environ['ANTHROPIC_AUTH_TOKEN']=='fixture-only.private-token'\n"
            "assert os.environ['ANTHROPIC_API_KEY']=='fixture-only.private-token'\n"
            "assert os.environ['ANTHROPIC_BASE_URL']=='https://open.bigmodel.cn/api/anthropic'\n"
            "assert os.environ['ANTHROPIC_MODEL']=='glm-5.3'\n"
            "assert not any('fixture-only.private-token' in arg for arg in sys.argv)\n"
            "assert '--bare' in sys.argv and '--restricted' in sys.argv\n"
            "assert sys.argv[sys.argv.index('--tools')+1]==''\n"
            "assert '--fallback-model' not in sys.argv\n"
            "assert 'CLAUDE_CODE_USE_VERTEX' not in os.environ\n"
            "assert 'OPENAI_API_KEY' not in os.environ\n"
            + extra +
            "print(json.dumps({'type':'result','subtype':'success','is_error':False,'num_turns':1,"
            "'result':'FUSION_GLM_OK','modelUsage':{'glm-5.3':{'inputTokens':10,'outputTokens':5}}}))\n")

    def test_isolated_launch_returns_success_without_private_output(self):
        cli = self.success_cli("print(os.environ['ANTHROPIC_AUTH_TOKEN'],file=sys.stderr)\n")
        with patch.dict(os.environ, {"OPENAI_API_KEY":"must-not-inherit", "CLAUDE_CODE_USE_VERTEX":"1"}):
            result = self.tool.probe(self.root, MODEL, cli, timeout=3)
        self.assertTrue(result["connection_verified"])
        self.assertFalse(result["billing_verified"])
        self.assertEqual(result["requested_model"], "glm-5.3")
        self.assertEqual(result["upstream_reported_model"], "unknown")
        self.assertNotIn(FAKE_KEY, json.dumps(result))

    def test_success_with_a_different_native_model_is_refused(self):
        cli = self.fake_cli("print('{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"num_turns\":1,\"result\":\"FUSION_GLM_OK\",\"modelUsage\":{\"claude-sonnet\":{}}}')\n")
        result = self.tool.probe(self.root, MODEL, cli, timeout=3)
        self.assertFalse(result["connection_verified"])
        self.assertEqual(result["status"], "native_model_mismatch")

    def test_native_error_does_not_leak_response_or_stderr(self):
        cli = self.fake_cli("import json,sys\nprint(json.dumps({'is_error':True,'api_error_status':401,'result':'fixture-only.private-token'}))\nprint('fixture-only.private-token',file=sys.stderr)\nsys.exit(1)\n")
        result = self.tool.probe(self.root, MODEL, cli, timeout=3)
        self.assertFalse(result["connection_verified"])
        self.assertEqual(result["api_error_status"], 401)
        self.assertNotIn(FAKE_KEY, json.dumps(result))

    def test_malformed_stdout_is_refused_without_echoing_it(self):
        cli = self.fake_cli("print('fixture-only.private-token')\n")
        result = self.tool.probe(self.root, MODEL, cli, timeout=3)
        self.assertEqual(result["status"], "invalid_native_result")
        self.assertNotIn(FAKE_KEY, json.dumps(result))

    def test_unrelated_reply_is_not_authentication_evidence(self):
        cli = self.fake_cli("print('{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"num_turns\":1,\"result\":\"Something else\",\"modelUsage\":{\"glm-5.3\":{}}}')\n")
        result = self.tool.probe(self.root, MODEL, cli, timeout=3)
        self.assertFalse(result["connection_verified"])
        self.assertEqual(result["status"], "unexpected_reply")

    def test_nontext_reply_is_refused_without_parser_crash(self):
        cli = self.fake_cli("print('{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"num_turns\":1,\"result\":{},\"modelUsage\":{\"glm-5.3\":{}}}')\n")
        result = self.tool.probe(self.root, MODEL, cli, timeout=3)
        self.assertEqual(result["status"], "unexpected_reply")

    def test_invalid_model_and_timeout_are_refused_before_launch(self):
        for model, timeout in [("sonnet",3),("glm-5.3\n--debug",3),("glm-5.3",0),("glm-5.3",121)]:
            with self.subTest(model=model,timeout=timeout):
                with self.assertRaises(ValueError):
                    self.tool.probe(self.root, model, "/does/not/exist", timeout)

    def test_timeout_stops_private_child_without_output(self):
        cli = self.fake_cli("import time\nprint('fixture-only.private-token',flush=True)\ntime.sleep(30)\n")
        result = self.tool.probe(self.root, MODEL, cli, timeout=0.1)
        self.assertEqual(result["status"], "timeout")
        self.assertFalse(result["connection_verified"])
        self.assertNotIn(FAKE_KEY, json.dumps(result))


if __name__ == "__main__":
    unittest.main()
