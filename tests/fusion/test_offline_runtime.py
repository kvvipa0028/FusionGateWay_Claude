"""Real synthetic process tests; no native model runtime or credential is used."""
import json
import os
from pathlib import Path
import select
import shutil
import signal
import subprocess
import tempfile
import time
import unittest

REPO=Path(__file__).resolve().parents[2]

class OfflineRuntimeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build=tempfile.TemporaryDirectory(prefix="fusion-fixture-build-")
        root=Path(cls.build.name); go=shutil.which("go")
        cls.binary=root/"fake-runtime"
        env={"PATH":str(Path(go).parent)+":/usr/bin:/bin","HOME":str(root),"GOENV":"off","GOTOOLCHAIN":"local","GOPROXY":"off","GOSUMDB":"off","GOMODCACHE":str(Path.home()/"go/pkg/mod"),"GOCACHE":str(Path.home()/"Library/Caches/go-build"),"CGO_ENABLED":"0"}
        subprocess.run([go,"build","-mod=readonly","-o",str(cls.binary),"./internal/fusion/testsupport/cmd/fake-runtime"],cwd=REPO,env=env,check=True,capture_output=True)
    @classmethod
    def tearDownClass(cls):
        cls.build.cleanup()
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory(prefix="fusion-fixture-process-")
        self.root=Path(self.tmp.name).resolve()
        self.workspace=self.root/"workspace";self.workspace.mkdir(mode=0o700)
        (self.workspace/".fixture-workspace").write_text("fusion-synthetic-workspace\n")
        (self.workspace/".fixture-workspace").chmod(0o600)
        self.env={"PATH":"/usr/bin:/bin","FUSION_FIXTURE_RUNTIME":"1"}
        for key,name in [("HOME","home"),("XDG_CONFIG_HOME","config"),("XDG_CACHE_HOME","cache"),("XDG_DATA_HOME","data"),("TMPDIR","tmp")]:
            folder=self.root/name;folder.mkdir(mode=0o700);self.env[key]=str(folder)
        self.children=[]
    def tearDown(self):
        for child in self.children:
            try: os.killpg(child.pid,signal.SIGKILL)
            except ProcessLookupError: pass
            child.wait(timeout=2)
            for stream in [child.stdin,child.stdout,child.stderr]:
                if stream: stream.close()
        self.tmp.cleanup()
    def start(self,mode):
        env=dict(self.env)
        env["FUSION_FIXTURE_HEARTBEAT"]=str(self.workspace/"heartbeat.txt")
        child=subprocess.Popen([str(self.binary)],env=env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,start_new_session=True,bufsize=0)
        self.children.append(child)
        child.stdin.write(json.dumps({"model":"fixture-model-a","account":"fixture-account-a","mode":mode,"revision":1,"workspace":str(self.workspace)}).encode())
        child.stdin.close();child.stdin=None
        return child
    def event(self,child):
        ready,_,_=select.select([child.stdout],[],[],2)
        self.assertTrue(ready,"fixture event timeout")
        return json.loads(child.stdout.readline())
    def test_real_cli_rpc_identity_and_order(self):
        child=self.start("rpc")
        out,err=child.communicate(timeout=3)
        self.assertEqual(child.returncode,0)
        events=[json.loads(x) for x in out.splitlines()]
        self.assertEqual([x["seq"] for x in events],[1,2,3,4])
        self.assertEqual(events[-1]["kind"],"completed")
        self.assertTrue(all(x["model"]=="fixture-model-a" and x["account"]=="fixture-account-a" for x in events))
        self.assertFalse(err)
    def test_real_cli_write_loss_preserves_effect_without_success(self):
        child=self.start("write_loss");out,_=child.communicate(timeout=3)
        self.assertEqual(child.returncode,70)
        self.assertEqual((self.workspace/"write-marker.txt").read_text(),"fixture-written\n")
        self.assertNotIn(b'"kind":"completed"',out)
    def test_real_grandchild_can_be_cancelled_as_process_group(self):
        child=self.start("grandchild")
        self.assertEqual(self.event(child)["kind"],"started")
        event=self.event(child);self.assertEqual(event["kind"],"child_started")
        self.assertGreater(event["pid"],0)
        marker=self.workspace/"heartbeat.txt"
        deadline=time.monotonic()+2
        while not marker.exists() and time.monotonic()<deadline: time.sleep(0.01)
        self.assertTrue(marker.exists(),"grandchild never ran")
        self.assertIsNone(child.poll(),"parent exited before cancellation")
        os.killpg(child.pid,signal.SIGTERM);child.wait(timeout=2)
        stamp=marker.stat().st_mtime_ns;time.sleep(0.06)
        self.assertEqual(marker.stat().st_mtime_ns,stamp,"grandchild continued writing")
        self.assertNotEqual(child.returncode,0)
    def test_cancel_before_completion_barrier(self):
        child=self.start("cancel_race")
        self.assertEqual(self.event(child)["kind"],"started")
        self.assertEqual(self.event(child)["kind"],"waiting")
        os.killpg(child.pid,signal.SIGTERM);out,_=child.communicate(timeout=2)
        self.assertNotIn(b'"kind":"completed"',out)
        self.assertNotEqual(child.returncode,0)
    def test_completion_after_release_barrier(self):
        child=self.start("cancel_race")
        self.assertEqual(self.event(child)["kind"],"started")
        self.assertEqual(self.event(child)["kind"],"waiting")
        (self.workspace/"release-marker").write_text("fixture-release\n")
        out,_=child.communicate(timeout=2)
        self.assertEqual(child.returncode,0)
        self.assertIn(b'"kind":"completed"',out)
    def test_binary_refuses_missing_fixture_environment(self):
        env=dict(self.env);del env["FUSION_FIXTURE_RUNTIME"]
        result=subprocess.run([str(self.binary)],env=env,capture_output=True,timeout=2)
        self.assertEqual(result.returncode,64)
        self.assertFalse(result.stdout)

if __name__=="__main__":unittest.main()
