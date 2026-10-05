"""Publisher proof must bind actual installed bytes, never grant generation."""
import dataclasses
import contextlib
import gzip
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import urllib.request
import unittest
from unittest.mock import patch

SCRIPT = Path(__file__).resolve().parents[1] / "verify-runtime-publishers.py"
spec = importlib.util.spec_from_file_location("publisher_probe", SCRIPT)
probe = None
if SCRIPT.exists():
    probe = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(probe)


class PublisherTests(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(probe, "explicit fixed-version publisher verifier missing")

    def test_pins_have_no_account_or_network_override(self):
        self.assertEqual(set(probe.PINS), {"codex", "grok", "claude"})
        self.assertEqual([probe.PINS[x].version for x in ("codex", "grok", "claude")],
                         ["0.160.0", "1.0.48", "2.1.287"])
        for p in probe.PINS.values():
            self.assertEqual(len(p.digest), 64)
            self.assertTrue(p.source.startswith("https://"))

    def test_literal_apple_requirement_rejects_other_publishers(self):
        for p in probe.PINS.values():
            command = probe.signature_command(Path("/tmp/test-executable"), p)
            self.assertEqual(command[:3], ["/usr/bin/codesign", "--verify", "--strict"])
            req = command[4]
            self.assertTrue(req.startswith("=anchor apple generic and "))
            self.assertIn(p.identifier, req)
            self.assertIn(p.team, req)
            self.assertIn("1.2.840.113635.100.6.1.13", req)

    def test_local_tamper_symlink_or_non_executable_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            p = root / "binary"
            data = b"\xcf\xfa\xed\xfe" + b"synthetic executable"
            p.write_bytes(data)
            p.chmod(0o700)
            pin = dataclasses.replace(probe.PINS["grok"], digest=hashlib.sha256(data).hexdigest())
            original = probe.local_identity(p, pin)
            p.write_bytes(data + b"changed")
            with self.assertRaises(ValueError): probe.local_identity(p, pin)
            p.write_bytes(data)
            link = root / "link"
            link.symlink_to(p)
            with self.assertRaises(ValueError): probe.local_identity(link, pin)
            p.chmod(0o600)
            with self.assertRaises(ValueError): probe.local_identity(p, pin)
            p.chmod(0o700)
            replacement = root / "replacement"
            replacement.write_bytes(data)
            replacement.chmod(0o700)
            replacement.replace(p)
            self.assertNotEqual(original, probe.local_identity(p, pin))

    def test_claude_manifest_requires_exact_version_size_and_checksum(self):
        pin = probe.PINS["claude"]
        doc = {"version": pin.version, "platforms": {"darwin-arm64":
            {"binary": "claude", "size": 23, "checksum": pin.digest}}}
        self.assertEqual(probe.claude_checksum(json.dumps(doc).encode(), pin, 23), pin.digest)
        for field, value in (("version", "2.1.288"), ("duplicate", True)):
            broken = json.dumps(doc).encode()
            if field == "version": broken = broken.replace(pin.version.encode(), value.encode())
            else: broken = broken[:-1] + b',"version":"2.1.287"}'
            with self.assertRaises(ValueError): probe.claude_checksum(broken, pin, 23)
        for field, value in (("size", 24), ("checksum", "0" * 64), ("binary", "other")):
            altered = json.loads(json.dumps(doc))
            altered["platforms"]["darwin-arm64"][field] = value
            with self.assertRaises(ValueError): probe.claude_checksum(json.dumps(altered).encode(), pin, 23)

    def test_codex_release_requires_exact_owned_asset_and_digest(self):
        url = probe.PINS["codex"].asset
        doc = {"tag_name": "rust-v0.160.0", "draft": False, "prerelease": False,
            "assets": [{"name": "codex-aarch64-apple-darwin.tar.gz",
                "browser_download_url": url, "digest": "sha256:" + "a" * 64, "size": 100}]}
        self.assertEqual(probe.codex_asset(json.dumps(doc).encode()), ("a" * 64, 100))
        for field, value in (("browser_download_url", "https://attacker.invalid/file"),
                             ("digest", None), ("size", True)):
            broken = json.loads(json.dumps(doc));broken["assets"][0][field] = value
            with self.assertRaises(ValueError): probe.codex_asset(json.dumps(broken).encode())
        doc["assets"] *= 2
        with self.assertRaises(ValueError): probe.codex_asset(json.dumps(doc).encode())

    def test_archive_hash_is_real_single_regular_binary_without_extraction(self):
        data = b"synthetic binary"
        for kind in ("valid", "duplicate", "link", "traversal"):
            stream = io.BytesIO()
            with tarfile.open(fileobj=stream, mode="w:gz") as archive:
                entry = tarfile.TarInfo("codex-aarch64-apple-darwin" if kind != "traversal" else "../codex-aarch64-apple-darwin")
                entry.size = len(data)
                if kind == "link": entry.type = tarfile.SYMTYPE;entry.linkname = "/private/key";entry.size = 0
                archive.addfile(entry, io.BytesIO(data))
                if kind == "duplicate": archive.addfile(entry, io.BytesIO(data))
            stream.seek(0)
            if kind == "valid": self.assertEqual(probe.codex_binary_hash(stream), hashlib.sha256(data).hexdigest())
            else:
                with self.assertRaises(ValueError): probe.codex_binary_hash(stream)

    def test_grok_compressed_bytes_must_match_whole_local_binary(self):
        data = b"synthetic binary"
        self.assertEqual(probe.grok_binary_hash(io.BytesIO(gzip.compress(data))), hashlib.sha256(data).hexdigest())
        with patch.object(probe, "MAX_BINARY", 10):
            with self.assertRaises(ValueError): probe.grok_binary_hash(io.BytesIO(gzip.compress(data)))

    def test_redirect_authority_has_no_http_foreign_bucket_or_credentials(self):
        for url in ("http://x.ai/cli/file", "https://user:password@x.ai/cli/file",
                    "https://x.ai.attacker.invalid/cli/file", "https://storage.googleapis.com/other/file",
                    "https://github.com/other/repo/releases/download/tag/file"):
            with self.assertRaises(ValueError): probe.check_url(url, "grok")
        probe.check_url(probe.PINS["grok"].source, "grok")
        with self.assertRaises(ValueError): probe.check_url("https://downloads.claude.ai/unrelated", "claude")

    def test_existing_report_refused_before_any_verification(self):
        with tempfile.TemporaryDirectory() as temp:
            destination = Path(temp) / "report.json"
            destination.write_text("existing evidence")
            with patch.object(probe, "verify_runtime") as verification, contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(probe.main(["--runtime", "claude", "--report", str(destination)]), 1)
                verification.assert_not_called()
            self.assertEqual(destination.read_text(), "existing evidence")

    def test_real_apple_verifier_accepts_pinned_vendor_and_refuses_wrong_team(self):
        p = probe.PINS["claude"]
        executable = Path.home() / p.relative_path
        if not executable.is_file() or not Path("/usr/bin/codesign").is_file():
            self.skipTest("explicit macOS Claude pin unavailable")
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            probe.run_check(probe.signature_command(executable, p), root)
            wrong = dataclasses.replace(p, team="5Y6N3AJ54S")
            with self.assertRaises(ValueError): probe.run_check(probe.signature_command(executable, wrong), root)

    def test_real_official_manifest_signature_rejects_changed_content_and_key(self):
        if not Path("/opt/homebrew/bin/gpg").exists():
            self.skipTest("explicit GPG verification tool unavailable")
        fixture = Path(__file__).parent / "fixtures/claude-2.1.287"
        manifest = (fixture / "manifest.json").read_bytes()
        signature = (fixture / "manifest.json.sig").read_bytes()
        key = (fixture / "key.asc").read_bytes()
        for content, public_key, succeeds in ((manifest, key, True),
                (manifest.replace(b"2.1.287", b"2.1.288"), key, False),
                (manifest, b"invalid public key", False)):
            with tempfile.TemporaryDirectory() as temp:
                root = Path(temp).resolve()
                if succeeds: probe.verify_manifest_signature(content, signature, public_key, root)
                else:
                    with self.assertRaises(ValueError): probe.verify_manifest_signature(content, signature, public_key, root)

    def test_redirect_handler_blocks_foreign_destination_before_request(self):
        client = probe.PublicReleaseClient("claude")
        handler = next(h for h in client.opener.handlers if isinstance(h, urllib.request.HTTPRedirectHandler))
        request = urllib.request.Request(probe.PINS["claude"].source)
        with self.assertRaises(ValueError):
            handler.redirect_request(request, None, 302, "Moved", {}, "https://attacker.invalid/artifact")

    def test_fetch_requires_complete_bounded_bytes_and_has_no_retry(self):
        class Response(io.BytesIO):
            status = 200
            url = probe.PINS["claude"].source

            def __init__(self, data, length):
                super().__init__(data)
                self.headers = {"Content-Length": str(length)}

        for data, length, maximum, success in ((b"1234", 4, 4, True),
                (b"12", 4, 4, False), (b"12345", 5, 4, False), (b"12345", 4, 4, False)):
            client = probe.PublicReleaseClient("claude")
            with patch.object(client.opener, "open", return_value=Response(data, length)) as opened:
                if success:
                    with client.fetch(probe.PINS["claude"].source, maximum) as result:
                        self.assertEqual(result.read(), data)
                    self.assertEqual(client.observations[0]["sha256"], hashlib.sha256(data).hexdigest())
                else:
                    with self.assertRaises(ValueError): client.fetch(probe.PINS["claude"].source, maximum)
                    self.assertEqual(client.observations, [])
                self.assertEqual(opened.call_count, 1)
                self.assertEqual(opened.call_args.args[0].header_items(),
                    [("User-agent", "FusionGateway-publisher-verification/1")])

    def test_expired_fetch_never_opens_network(self):
        client = probe.PublicReleaseClient("claude")
        client.deadline = 0
        with patch.object(client.opener, "open") as opened:
            with self.assertRaises(ValueError): client.fetch(probe.PINS["claude"].source, 100)
            opened.assert_not_called()

    def test_replaced_file_during_public_download_cannot_emit_success(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            pin = probe.PINS["grok"]
            binary = root / pin.relative_path
            binary.parent.mkdir(parents=True)
            data = b"\xcf\xfa\xed\xfe" + b"synthetic binary"
            binary.write_bytes(data);binary.chmod(0o700)
            pin = dataclasses.replace(pin, digest=hashlib.sha256(data).hexdigest())

            def download(*args):
                replacement = binary.with_name("replacement")
                replacement.write_bytes(data);replacement.chmod(0o700);replacement.replace(binary)
                return io.BytesIO(gzip.compress(data))

            with patch.object(Path, "home", return_value=root), patch.dict(probe.PINS, {"grok": pin}), \
                 patch.object(probe, "run_check", return_value=""), \
                 patch.object(probe.PublicReleaseClient, "fetch", side_effect=download):
                with self.assertRaises(ValueError): probe.verify_runtime("grok")


if __name__ == "__main__":
    unittest.main(verbosity=2)
