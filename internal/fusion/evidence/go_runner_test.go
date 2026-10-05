//go:build darwin

package evidence

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/workspace"
)

var goRecordCapture = flag.String("fusion-go-evidence-record", "", "capture actual owned Go backend record for schema checks")

const testGoRoot = "/Users/zhaojianzhi/.local/share/go/1.26.3"

func goFixture(t *testing.T, code string) (workspace.FrozenArtifact, Spec, string) {
	return goFixtureFiles(t, code, nil)
}

func goFixtureFiles(t *testing.T, code string, extra map[string]string) (workspace.FrozenArtifact, Spec, string) {
	t.Helper()
	source, root := private(t), private(t)
	files := map[string]string{"go.mod": "module fusion.test/local\n\ngo 1.26.3\n", "example_test.go": code}
	for name, content := range extra {
		files[name] = content
	}
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(source, name)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	input, err := workspace.Copy(source, root, "source")
	if err != nil {
		t.Fatal(err)
	}
	a, err := workspace.Freeze(input, root, "frozen")
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(testGoRoot, "bin/go")
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	s := Spec{Tool: Tool{Executable: exe, SHA256: digest(b), Version: "go version go1.26.3 darwin/arm64", VersionArgs: []string{"version"}}, Args: []string{"test", "-count=1", "-json", "."}, SuitePaths: []string{"example_test.go"}, Rules: Rules{MinTests: 1}, Timeout: 120 * time.Second}
	h, err := GoRootHash(testGoRoot)
	if err != nil {
		t.Fatal(err)
	}
	s.Go = &GoToolchain{Root: testGoRoot, RootHash: h, Package: "."}
	s.Args = goLogicalArgs(s.Go)
	return a, s, source
}

func TestGoRunnerActuallyBuildsAndRunsCurrentArtifact(t *testing.T) {
	a, s, _ := goFixture(t, "package local\nimport \"testing\"\nfunc TestReal(t *testing.T){if 2+2!=4{t.Fatal(\"bad\")}}\n")
	result, err := Run(context.Background(), a, private(t), s)
	if err != nil {
		t.Fatal("real Go backend unavailable", err)
	}
	if *goRecordCapture != "" {
		b, _ := json.MarshalIndent(result.Record(), "", "  ")
		if e := os.WriteFile(*goRecordCapture, append(b, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
	verdict := Evaluate(result, a, s)
	if verdict.Status != Passed || verdict.Tests != 1 || len(result.Record().GoCommands) != 5 || !result.Record().Executed || !result.Record().Stopped {
		t.Fatal("real compiled Go test did not pass", verdict, result.Record().ExitCode)
	}
}

func TestGoRunnerActualFailuresZeroSkipCompileAndMissingDependencies(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		want       Status
	}{
		{"failure", `package local;import "testing";func TestBad(t *testing.T){t.Fatal("model says passed")}`, Failed},
		{"zero", `package local;import ("testing";"os");func TestMain(m *testing.M){os.Exit(0)};func TestNever(t *testing.T){t.Fatal("never")}`, Unverified},
		{"skipped", `package local;import "testing";func TestSkip(t *testing.T){t.Skip("unavailable")}`, Unverified},
		{"compile", `package local;this is not valid Go`, Failed},
		{"missingdependency", `package local;import (_ "unavailable.invalid/private/module";"testing");func TestMissing(t *testing.T){}`, Failed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, s, _ := goFixture(t, tc.code)
			r, e := Run(context.Background(), a, private(t), s)
			if e != nil {
				t.Fatal(e)
			}
			v := Evaluate(r, a, s)
			if TestsExecuted(r.Record()) != (tc.name != "compile" && tc.name != "missingdependency") {
				t.Fatal("compilation mislabeled as tests executed")
			}
			if v.Status != tc.want {
				out, errout := r.Logs()
				t.Fatalf("%s: %v %s %s", tc.name, v, out, errout)
			}
			stored, e := Export(r, a, s)
			if e != nil {
				t.Fatal("owned failure not persistable", e)
			}
			b, _ := json.Marshal(stored)
			var restored Stored
			if json.Unmarshal(b, &restored) != nil || EvaluateStored(restored, a, s).Status != tc.want {
				t.Fatal("failure changed across stored receipt")
			}
		})
	}
}
func TestGoRunnerKernelBoundariesAndActualOwnedReceipt(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	secret := filepath.Join(private(t), "private-key")
	if os.WriteFile(secret, []byte("synthetic secret"), 0600) != nil {
		t.Fatal("fixture")
	}
	t.Setenv("ANTHROPIC_API_KEY", "synthetic parent secret")
	t.Setenv("FUSION_PARENT_SECRET", "synthetic parent secret")
	code := fmt.Sprintf(`package local
 import("testing";"os";"os/exec";"path/filepath";"net";"time")
 func TestChild(t *testing.T){}
 func TestBoundary(t *testing.T){
  if os.Getenv("ANTHROPIC_API_KEY")!=""||os.Getenv("FUSION_PARENT_SECRET")!=""{t.Fatal("inherited credential")}
  if e:=os.WriteFile("unauthorized",[]byte("bad"),0600);e==nil{t.Fatal("input write")}
  if _,e:=os.ReadFile(%q);e==nil{t.Fatal("private read")}
  if e:=os.WriteFile(filepath.Join(os.Getenv("TMPDIR"),"positive"),[]byte("ok"),0600);e!=nil{t.Fatal("own scratch denied",e)}
  if e:=exec.Command(os.Args[0],"-test.run=^TestChild$").Run();e==nil{t.Fatal("untrusted fork")}
  if c,e:=net.DialTimeout("tcp",%q,time.Second);e==nil{c.Close();t.Fatal("network")}
  if l,e:=net.Listen("tcp","127.0.0.1:0");e==nil{l.Close();t.Fatal("network listener")}
 }`, secret, listener.Addr().String())
	a, s, _ := goFixture(t, code)
	r, e := Run(context.Background(), a, private(t), s)
	if e != nil {
		t.Fatal(e)
	}
	if v := Evaluate(r, a, s); v.Status != Passed || v.Tests != 2 {
		out, errout := r.Logs()
		t.Fatal(v, string(out), string(errout))
	}
	original := r.Record()
	if len(original.GoCommands) != 5 {
		t.Fatal("missing actual command chain")
	}
	for _, c := range original.GoCommands {
		if !c.Executed || !c.Stopped || c.ExitCode != 0 {
			t.Fatal("unverified phase")
		}
	}
	saved, e := Export(r, a, s)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(saved)
	var restored Stored
	if json.Unmarshal(b, &restored) != nil || EvaluateStored(restored, a, s).Status != Passed {
		t.Fatal("owned persistent Go receipt")
	}
	changed := r.Record()
	changed.Spec.Go.Root = "/forged"
	changed.GoCommands[0].Args[0] = "forged"
	changed.GoCommands[0].Environment[0] = "forged"
	if r.Record().Spec.Go.Root != testGoRoot || r.Record().GoCommands[0].Args[0] != "version" || strings.HasPrefix(r.Record().GoCommands[0].Environment[0], "forged") {
		t.Fatal("record getter aliased trusted state")
	}
	bad := clone(s)
	bad.Go.Package = "./other"
	if Evaluate(r, a, bad).Status != Superseded {
		t.Fatal("changed package retained proof")
	}
	for _, mutate := range []func(*Stored){
		func(v *Stored) { v.Record.GoCommands[1].Args[0] = "run" },
		func(v *Stored) { v.Record.GoCommands[2].Executable = "/bin/sh" },
		func(v *Stored) { v.Record.GoCommands[3].Args = append(v.Record.GoCommands[3].Args, "-test.run=Never") },
		func(v *Stored) { v.Record.GoCommands[4].Args[1] = "./other" },
		func(v *Stored) { v.Record.GoCommands[3].Environment[0] = "PATH=/private" },
		func(v *Stored) { v.Record.GoCommands[3].StartedAt = v.Record.GoCommands[2].StartedAt },
		func(v *Stored) { v.Record.GoCommands = v.Record.GoCommands[:3] },
	} {
		var corrupt Stored
		if json.Unmarshal(b, &corrupt) != nil {
			t.Fatal("fixture")
		}
		mutate(&corrupt)
		if ValidStored(corrupt) {
			t.Fatal("corrupt actual command trace accepted")
		}
	}
}
func TestGoSpecNoFlagsEnvironmentShellOrEscapingPackage(t *testing.T) {
	a, s, _ := goFixture(t, `package local;import "testing";func TestGood(t *testing.T){}`)
	for _, mutate := range []func(*Spec){
		func(s *Spec) { s.Args = append(s.Args, "-toolexec=/bin/sh") },
		func(s *Spec) { s.Go.Package = "../outside" }, func(s *Spec) { s.Go.Package = "./..." },
		func(s *Spec) { s.Go.RootHash = strings.Repeat("a", 64) }, func(s *Spec) { s.Tool.Version = "go version unknown" },
	} {
		bad := clone(s)
		mutate(&bad)
		if _, e := Run(context.Background(), a, private(t), bad); e == nil {
			t.Fatal("unapproved backend expanded")
		}
	}
}

func TestGoRunnerFrozenVendorDependencyActuallyRunsOffline(t *testing.T) {
	a, s, _ := goFixtureFiles(t, `package local;import ("testing";"example.invalid/value");func TestVendor(t *testing.T){if value.Answer()!=42{t.Fatal("vendor not consumed")}}`, map[string]string{
		"go.mod":                                "module fusion.test/local\n\ngo 1.26.3\n\nrequire example.invalid/value v1.0.0\n",
		"vendor/modules.txt":                    "# example.invalid/value v1.0.0\n## explicit; go 1.26.3\nexample.invalid/value\n",
		"vendor/example.invalid/value/value.go": "package value\nfunc Answer()int{return 42}\n",
	})
	s.Go.Vendor = true
	s.Args = goLogicalArgs(s.Go)
	r, err := Run(context.Background(), a, private(t), s)
	if err != nil || Evaluate(r, a, s).Status != Passed || Evaluate(r, a, s).Tests != 1 {
		out, errout := r.Logs()
		t.Fatal("frozen offline vendor unavailable", err, Evaluate(r, a, s), string(out), string(errout))
	}
}

func TestGoRunnerActualCompiledTestStopsOnCancelAndSourceRevocation(t *testing.T) {
	for _, mode := range []string{"cancel", "source"} {
		t.Run(mode, func(t *testing.T) {
			a, s, source := goFixture(t, `package local;import("testing";"os";"path/filepath";"strconv";"time");func TestWait(t *testing.T){if os.WriteFile(filepath.Join(os.Getenv("TMPDIR"),"ready"),[]byte(strconv.Itoa(os.Getpid())),0600)!=nil{t.Fatal("ready")};for{time.Sleep(time.Second)}}`)
			root := private(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type outcome struct {
				result Result
				err    error
			}
			done := make(chan outcome, 1)
			go func() { r, e := Run(ctx, a, root, s); done <- outcome{r, e} }()
			pid := 0
			deadline := time.Now().Add(60 * time.Second)
			for time.Now().Before(deadline) {
				entries, _ := os.ReadDir(root)
				for _, entry := range entries {
					raw, e := os.ReadFile(filepath.Join(root, entry.Name(), "test/tmp/ready"))
					if e == nil {
						pid, _ = strconv.Atoi(string(raw))
					}
				}
				if pid > 0 {
					break
				}
				select {
				case out := <-done:
					t.Fatal("compiled test never started", out.err)
				default:
				}
				time.Sleep(20 * time.Millisecond)
			}
			id := identity(pid)
			if pid == 0 || id == 0 {
				cancel()
				<-done
				t.Fatal("actual compiled test not alive")
			}
			if mode == "cancel" {
				cancel()
			} else if e := os.WriteFile(filepath.Join(source, "example_test.go"), []byte("package changed"), 0600); e != nil {
				cancel()
				<-done
				t.Fatal(e)
			}
			select {
			case out := <-done:
				if out.err == nil && Evaluate(out.result, a, s).Status == Passed {
					t.Fatal("interrupted test passed")
				}
			case <-time.After(10 * time.Second):
				cancel()
				t.Fatal("compiled test failed to stop")
			}
			if identity(pid) == id {
				t.Fatal("actual compiled test survived")
			}
		})
	}
}

func TestGoRunnerActualOutputTruncationCannotPass(t *testing.T) {
	a, s, _ := goFixture(t, `package local;import("testing";"fmt";"strings");func TestLarge(t *testing.T){fmt.Print(strings.Repeat("x",2<<20))}`)
	r, e := Run(context.Background(), a, private(t), s)
	if e != nil || !r.Record().Truncated || !TestsExecuted(r.Record()) || Evaluate(r, a, s).Status != Unverified {
		t.Fatal("actual truncated Go report passed", e, Evaluate(r, a, s))
	}
}

func TestGoSDKFreezeRejectsSymlinksPermissionsAndContentDrift(t *testing.T) {
	root := private(t)
	file := filepath.Join(root, "tool")
	if os.WriteFile(file, []byte("pinned"), 0700) != nil {
		t.Fatal("fixture")
	}
	h, e := GoRootHash(root)
	if e != nil {
		t.Fatal(e)
	}
	if os.WriteFile(file, []byte("edited"), 0700) != nil || goRootCurrent(&GoToolchain{Root: root, RootHash: h}) {
		t.Fatal("SDK contents retained authority")
	}
	if os.Chmod(file, 0722) != nil {
		t.Fatal("fixture")
	}
	if _, e := GoRootHash(root); e == nil {
		t.Fatal("writable compiler accepted")
	}
	if os.Chmod(file, 0700) != nil || os.Symlink(file, filepath.Join(root, "link")) != nil {
		t.Fatal("fixture")
	}
	if _, e := GoRootHash(root); e == nil {
		t.Fatal("SDK symlink accepted")
	}
}
