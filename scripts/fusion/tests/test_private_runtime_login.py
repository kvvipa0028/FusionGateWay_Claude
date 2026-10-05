"""Private login prepares an isolated terminal, never a generation grant."""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import socket
import signal
import subprocess
import tempfile
import unittest
from unittest.mock import patch

SCRIPT = Path(__file__).resolve().parents[1] / "private-runtime-login.py"
spec = importlib.util.spec_from_file_location("private_login_test_module", SCRIPT)
login = None
if SCRIPT.exists():
    login = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(login)


class PrivateLoginTests(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(login, "isolated official login entry missing")
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve() / "private-login"

    def test_prepare_two_runtime_homes_without_creating_credentials(self):
        layouts = [login.prepare(self.root / name, name) for name in ("codex", "grok")]
        for layout in layouts:
            self.assertEqual(layout.root.stat().st_mode & 0o777, 0o700)
            self.assertEqual(layout.home.stat().st_mode & 0o777, 0o700)
            self.assertEqual(layout.config.stat().st_mode & 0o777, 0o600)
            self.assertFalse(layout.credential.exists())
            state = login.status(layout)
            self.assertFalse(state["credential_file_present"])
            self.assertFalse(state["authenticated"])
            self.assertFalse(state["generation_admitted"])
        self.assertNotEqual(layouts[0].home, layouts[1].home)

    def test_seed_drift_refused_without_overwriting_user_or_auth(self):
        layout = login.prepare(self.root, "codex")
        layout.config.write_text('cli_auth_credentials_store = "keyring"\n')
        auth = b'{"private-test-token":"do-not-print"}'
        layout.credential.write_bytes(auth);layout.credential.chmod(0o600)
        with self.assertRaises(ValueError): login.prepare(self.root, "codex")
        self.assertEqual(layout.config.read_text(), 'cli_auth_credentials_store = "keyring"\n')
        self.assertEqual(layout.credential.read_bytes(), auth)

    def test_symlink_git_root_or_loose_directory_refused(self):
        target = self.root.parent / "target"
        target.mkdir(mode=0o700)
        self.root.symlink_to(target)
        with self.assertRaises(ValueError): login.prepare(self.root, "grok")
        self.root.unlink()
        self.root.mkdir(mode=0o700)
        (self.root / ".git").mkdir()
        with self.assertRaises(ValueError): login.prepare(self.root, "grok")
        (self.root / ".git").rmdir();self.root.chmod(0o755)
        with self.assertRaises(ValueError): login.prepare(self.root, "grok")

    def test_cache_presence_does_not_claim_login_or_parse_credentials(self):
        layout = login.prepare(self.root, "grok")
        secret = b"not-even-json PRIVATE_TEST_TOKEN"
        layout.credential.write_bytes(secret);layout.credential.chmod(0o600)
        state = login.status(layout)
        self.assertTrue(state["credential_file_present"])
        self.assertFalse(state["authenticated"])
        self.assertFalse(state["identity_verified"])
        self.assertNotIn("PRIVATE_TEST_TOKEN", json.dumps(state))

    def test_link_hardlink_or_public_credential_cache_refused(self):
        layout = login.prepare(self.root, "codex")
        target = self.root / "elsewhere"
        target.write_text("PRIVATE_TEST_TOKEN");target.chmod(0o600)
        layout.credential.symlink_to(target)
        with self.assertRaises(ValueError): login.status(layout)
        layout.credential.unlink();os.link(target, layout.credential)
        with self.assertRaises(ValueError): login.status(layout)
        layout.credential.unlink();layout.credential.write_text("private");layout.credential.chmod(0o644)
        with self.assertRaises(ValueError): login.status(layout)

    def test_environment_does_not_forward_ambient_auth_proxy_or_oidc(self):
        layout = login.prepare(self.root, "grok")
        with patch.dict(os.environ, {"XAI_API_KEY":"AUTH_TEST_SENTINEL", "OPENAI_API_KEY":"AUTH_TEST_SENTINEL",
                "HTTPS_PROXY":"https://attacker.invalid", "GROK_OIDC_ISSUER":"https://attacker.invalid",
                "CODEX_HOME":"/daily-client", "HOME":"/daily-client"}):
            env = login.environment(layout)
        self.assertEqual(env["HOME"], str(layout.home))
        self.assertEqual(env["GROK_HOME"], str(layout.home / ".grok"))
        self.assertEqual(env["NO_PROXY"], "*")
        self.assertEqual(env["no_proxy"], "*")
        self.assertNotIn("AUTH_TEST_SENTINEL", json.dumps(env))
        for key in ("XAI_API_KEY", "OPENAI_API_KEY", "HTTPS_PROXY", "GROK_OIDC_ISSUER"):
            self.assertNotIn(key, env)

    def test_codex_uses_verified_public_system_ca_not_ambient_ca(self):
        layout = login.prepare(self.root, "codex")
        with patch.dict(os.environ, {"CODEX_CA_CERTIFICATE":"/untrusted-ca", "SSL_CERT_FILE":"/untrusted-ca"}):
            env = login.environment(layout)
        self.assertEqual(env["SSL_CERT_FILE"], "/private/etc/ssl/cert.pem")
        self.assertNotIn("CODEX_CA_CERTIFICATE", env)
        self.assertNotIn("untrusted-ca", json.dumps(env))
        self.assertEqual(login.system_ca_identity(), login.system_ca_identity())
        link = self.root.parent / "ca-link.pem"
        link.symlink_to("/private/etc/ssl/cert.pem")
        with self.assertRaises(ValueError):login.system_ca_identity(link)
        owned = self.root.parent / "user-ca.pem"
        owned.write_text("-----BEGIN CERTIFICATE-----\nfixture\n-----END CERTIFICATE-----\n")
        with self.assertRaises(ValueError):login.system_ca_identity(owned)

    def test_only_official_device_flow_command_is_available(self):
        for runtime in ("codex", "grok"):
            layout = login.prepare(self.root / runtime, runtime)
            command = login.native_command(layout, "login")
            self.assertIn("--device-auth", command)
            self.assertNotIn("--with-api-key", command)
            if runtime == "codex":
                self.assertIn('cli_auth_credentials_store="file"', command)
                self.assertIn('forced_login_method="chatgpt"', command)
            else:self.assertNotIn("--oauth", command)  # Mutually exclusive in pinned Grok 1.0.48.
            for mode in ("exec", "logout", "custom", "status"):
                with self.assertRaises(ValueError): login.native_command(layout, mode)

    def test_noninteractive_login_refused_before_prepare_or_native(self):
        output = io.StringIO()
        with patch.object(login.sys.stdin, "isatty", return_value=False), \
             patch.object(login, "prepare") as prepare, contextlib.redirect_stdout(output):
            result = login.main(["login", "--runtime", "codex"])
        self.assertEqual(result, 1)
        prepare.assert_not_called()
        self.assertNotIn("PRIVATE_TEST_TOKEN", output.getvalue())

    def test_redirected_stderr_refused_before_prepare_or_native(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output), \
             patch.object(login.sys.stdin, "isatty", return_value=True), \
             patch.object(login.sys.stdout, "isatty", return_value=True), \
             patch.object(login.sys.stderr, "isatty", return_value=False), \
             patch.object(login, "prepare") as prepare:
            self.assertEqual(login.main(["login", "--runtime", "codex"]), 1)
            prepare.assert_not_called()

    def test_real_kernel_boundaries_with_unconfined_positive_control(self):
        source = SCRIPT.parents[2] / "tests/fusion/fixtures/codex-preferences-probe.c"
        probe = self.root.parent / "private-login-boundary-probe"
        compiled = subprocess.run(["/usr/bin/clang", "-Wall", "-Wextra", "-Werror",
            "-framework", "CoreFoundation", str(source), "-o", str(probe)],
            capture_output=True, timeout=30)
        self.assertEqual(compiled.returncode, 0, "kernel fixture compilation failed")
        forbidden = self.root.parent / "outside-auth-home.txt"
        forbidden.write_text("synthetic outside-home positive control")
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0));listener.listen(4)
            port = listener.getsockname()[1]
            self.assertNotIn(port, (53, 443))
            positive = subprocess.run([str(probe), "--unconfined", str(forbidden), str(port)],
                capture_output=True, timeout=15)
            self.assertEqual(positive.returncode, 0, "unconfined syscall positive control failed")
            for online in (False, True):
                with self.subTest(online=online):
                    layout = login.prepare(self.root / str(online), "codex")
                    confined = subprocess.run(["/usr/bin/sandbox-exec", "-p",
                        login.profile(layout, probe, online), str(probe), "--confined",
                        str(forbidden), str(port)], cwd=layout.root / "cwd",
                        env=login.environment(layout), capture_output=True, timeout=15)
                    self.assertEqual(confined.returncode, 0, "real kernel auth boundary failed")
                    self.assertEqual(confined.stdout, b"fixture-completed\n")
                    self.assertEqual((layout.home / ".codex/private-probe.txt").read_text(), "fixture")
                    self.assertFalse((layout.root / "cwd/readonly-effect.txt").exists())

    def test_cancel_reaps_real_sandbox_process_and_handles_group_exit_race(self):
        source = self.root.parent / "bounded-login-wait.c"
        source.write_text('#include <unistd.h>\nint main(void) { alarm(5); if (write(1,"ready\\n",6)!=6) return 1; for (;;) pause(); }\n')
        probe = self.root.parent / "bounded-login-wait"
        compiled = subprocess.run(["/usr/bin/clang", "-Wall", "-Wextra", "-Werror",
            "-isysroot", subprocess.run(["/usr/bin/xcrun", "--show-sdk-path"],
                capture_output=True, text=True, check=True, timeout=15).stdout.strip(),
            str(source), "-o", str(probe)], capture_output=True, timeout=30)
        self.assertEqual(compiled.returncode, 0)
        layout = login.prepare(self.root, "codex")
        child = subprocess.Popen(["/usr/bin/sandbox-exec", "-p", login.profile(layout, probe, False),
            str(probe)], cwd=layout.root / "cwd", env=login.environment(layout),
            start_new_session=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            self.assertEqual(child.stdout.readline(), b"ready\n")
            with patch.object(login.os, "killpg", side_effect=PermissionError):
                login.send_owned_signal(child, signal.SIGTERM)
            child.wait(timeout=3)
            self.assertEqual(child.returncode, -signal.SIGTERM)
            login.send_owned_signal(child, signal.SIGTERM)  # Terminal means no fresh signal.
        finally:
            if child.poll() is None:child.wait(timeout=6)  # Fixture alarm bounds a failed test.
            child.stdout.close();child.stderr.close()

    def test_system_dns_socket_allowed_only_for_online_login(self):
        source = self.root.parent / "login-dns-probe.c"
        source.write_text('''#include <CoreFoundation/CoreFoundation.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>
#include <string.h>
int main(int argc, char **argv) {
    if (argc != 2) return 80;
    struct sockaddr_un address = {0};
    address.sun_family = AF_UNIX;
    strcpy(address.sun_path, "/private/var/run/mDNSResponder");
    int fd = socket(AF_UNIX, SOCK_STREAM, 0);
    int ok = fd >= 0 && connect(fd, (struct sockaddr *)&address, sizeof(address)) == 0;
    if (fd >= 0) close(fd);
    return ok == (argv[1][0] == '1') ? 0 : 81;
}
''')
        probe = self.root.parent / "login-dns-probe"
        compiled = subprocess.run(["/usr/bin/clang", "-Wall", "-Wextra", "-Werror",
            "-framework", "CoreFoundation", str(source), "-o", str(probe)],
            capture_output=True, timeout=30)
        self.assertEqual(compiled.returncode, 0)
        positive = subprocess.run([str(probe), "1"], capture_output=True, timeout=15)
        self.assertEqual(positive.returncode, 0, "system DNS socket positive control failed")
        for runtime in ("codex", "grok"):
            layout = login.prepare(self.root / runtime, runtime)
            for online in (False, True):
                with self.subTest(runtime=runtime, online=online):
                    result = subprocess.run(["/usr/bin/sandbox-exec", "-p",
                        login.profile(layout, probe, online), str(probe), "1" if online else "0"],
                        cwd=layout.root / "cwd", env=login.environment(layout),
                        capture_output=True, timeout=15)
                    self.assertEqual(result.returncode, 0, "system DNS socket boundary failed")


if __name__ == "__main__":
    unittest.main(verbosity=2)
