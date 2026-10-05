//go:build darwin

package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/workspace"
)

var captureRecord = flag.String("fusion-evidence-record", "", "capture an actual synthetic runner record for offline schema verification")

// A real sandboxed process emits the captured report; fixtures never inject
// reports into the gate. The test binary exits before testing prints PASS.
func TestEvidenceWorker(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--" {
			index = i
			break
		}
	}
	if index < 0 || index+1 >= len(os.Args) {
		return
	}
	mode := os.Args[index+1]
	if mode == "version" {
		fmt.Print("evidence-test-tool 1\n")
		os.Exit(0)
	}
	if mode == "wait" {
		for {
			time.Sleep(time.Second)
		}
	}
	if mode == "truncated" {
		fmt.Print(strings.Repeat("x", 2<<20))
		os.Exit(0)
	}
	if mode == "malformed" {
		fmt.Print("<testsuite>")
		os.Exit(0)
	}
	if mode == "zero" {
		fmt.Print(`<testsuite tests="0" failures="0" errors="0" skipped="0"/>`)
		os.Exit(0)
	}
	if mode == "inconsistent" {
		fmt.Print(`<testsuite tests="3" failures="0" errors="0" skipped="0"><testcase name="one"/></testsuite>`)
		os.Exit(0)
	}
	if mode == "skipped" {
		fmt.Print(`<testsuite tests="1" failures="0" errors="0" skipped="1"><testcase name="one"><skipped/></testcase></testsuite>`)
		os.Exit(0)
	}
	if mode == "failed" {
		fmt.Print(`<testsuite tests="1" failures="1" errors="0" skipped="0"><testcase name="one"><failure>LLM says passed</failure></testcase></testsuite>`)
		os.Exit(0)
	}
	if mode == "exit" {
		fmt.Print(`<testsuite tests="1" failures="0" errors="0" skipped="0"><testcase name="one"/></testsuite>`)
		os.Exit(7)
	}
	if mode != "passed" && mode != "boundary" {
		os.Exit(82)
	}
	if b, err := os.ReadFile("tests/input.txt"); err != nil || string(b) != "actual test input\n" {
		os.Exit(83)
	}
	if mode == "boundary" {
		if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("FUSION_PARENT_SECRET") != "" {
			os.Exit(84)
		}
		if err := os.WriteFile("tests/unauthorized.txt", []byte("bad"), 0600); err == nil {
			os.Exit(85)
		}
		if err := os.WriteFile(filepath.Join(os.Getenv("TMPDIR"), "positive"), []byte("ok"), 0600); err != nil {
			os.Exit(86)
		}
		// This executable is allowed by process-exec; this proves fork denial.
		if err := exec.Command(os.Args[0], "-test.run=^TestEvidenceWorker$", "--", "passed").Run(); err == nil {
			os.Exit(87)
		}
		if listener, err := net.Listen("tcp", "127.0.0.1:0"); err == nil {
			listener.Close()
			os.Exit(88)
		}
		if _, err := os.ReadFile(os.Args[index+2]); err == nil {
			os.Exit(89)
		}
		if connection, err := net.DialTimeout("tcp", os.Args[index+3], time.Second); err == nil {
			connection.Close()
			os.Exit(90)
		}
	}
	fmt.Print(`<testsuite tests="1" failures="0" errors="0" skipped="0"><testcase name="actual"/></testsuite>`)
	os.Exit(0)
}

func private(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(p, 0700); err != nil {
		t.Fatal(err)
	}
	return p
}

func fixture(t *testing.T, mode string) (workspace.FrozenArtifact, Spec, string) {
	t.Helper()
	source, root := private(t), private(t)
	for _, p := range []string{source, root} {
		if err := os.Chmod(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(source, "tests"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "tests/input.txt"), []byte("actual test input\n"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := workspace.Copy(source, root, "producer")
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := workspace.Freeze(snapshot, root, "artifact")
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(bytes)
	spec := Spec{Tool: Tool{Executable: exe, SHA256: hex.EncodeToString(hash[:]), Version: "evidence-test-tool 1", VersionArgs: []string{"-test.run=^TestEvidenceWorker$", "--", "version"}}, Args: []string{"-test.run=^TestEvidenceWorker$", "--", mode}, SuitePaths: []string{"tests"}, Rules: Rules{MinTests: 1}, Timeout: 2 * time.Second}
	return artifact, spec, source
}

func TestRunnerActualProcessBindsCodeSuiteCommandAndVersion(t *testing.T) {
	artifact, spec, _ := fixture(t, "passed")
	result, err := Run(context.Background(), artifact, private(t), spec)
	if err != nil {
		t.Fatal(err)
	}
	record := result.Record()
	if *captureRecord != "" {
		raw, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(*captureRecord, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		out, errout := result.Logs()
		if err = os.WriteFile(*captureRecord+".stdout", out, 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(*captureRecord+".stderr", errout, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if record.ExitCode != 0 || !record.Executed || !record.Stopped || record.ToolVersion != spec.Tool.Version || record.ArtifactHash != artifact.Manifest().TreeHash || record.SuiteHash == "" || record.SpecHash == "" || record.ReportHash == "" || record.StartedAt.IsZero() || !record.FinishedAt.After(record.StartedAt) {
		t.Fatal(record)
	}
	if got := Evaluate(result, artifact, spec); got.Status != Passed || got.Tests != 1 {
		t.Fatal(got)
	}
	// Getters expose copies, never the owned receipt used by the gate.
	record.Spec.Args[0] = "forged"
	record.Environment[0] = "ANTHROPIC_API_KEY=forged"
	stdout, stderr := result.Logs()
	stdout[0] = '!'
	if len(stderr) > 0 {
		stderr[0] = '!'
	}
	if got := Evaluate(result, artifact, spec); got.Status != Passed {
		t.Fatal("mutable receipt", got)
	}
	// Exported metadata, even genuine, is not a runner-owned execution receipt.
	if got := Evaluate(Result{}, artifact, spec); got.Status != Unverified {
		t.Fatal(got)
	}
	changed := spec
	changed.Rules.MinTests = 2
	if got := Evaluate(result, artifact, changed); got.Status != Superseded {
		t.Fatal(got)
	}
	other, _, _ := fixture(t, "passed")
	if got := Evaluate(result, other, spec); got.Status != Superseded {
		t.Fatal("another source's equal bytes reused this execution", got)
	}
	nextRoot := private(t)
	copy, err := other.Copy(nextRoot, "changed")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copy.Path, "tests/input.txt"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	changedArtifact, err := workspace.Freeze(copy, nextRoot, "new-artifact")
	if err != nil {
		t.Fatal(err)
	}
	if got := Evaluate(result, changedArtifact, spec); got.Status != Superseded {
		t.Fatal(got)
	}
}

func TestRunnerActualReportFailuresCannotBecomePassed(t *testing.T) {
	for _, mode := range []string{"exit", "failed", "zero", "skipped", "malformed", "inconsistent", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			artifact, spec, _ := fixture(t, mode)
			result, err := Run(context.Background(), artifact, private(t), spec)
			if err != nil {
				t.Fatal(err)
			}
			want := Unverified
			if mode == "exit" || mode == "failed" {
				want = Failed
			}
			if got := Evaluate(result, artifact, spec); got.Status != want || !result.Record().Executed || !result.Record().Stopped {
				t.Fatal(got, result.Record())
			}
			if mode == "exit" && result.Record().ExitCode != 7 {
				t.Fatal("actual exit code lost", result.Record())
			}
			if mode == "truncated" && !result.Record().Truncated {
				t.Fatal("truncation lost")
			}
		})
	}
}

func TestRunnerActualKernelBoundariesAndPositiveScratch(t *testing.T) {
	artifact, spec, _ := fixture(t, "boundary")
	forbidden := filepath.Join(private(t), "secret")
	if err := os.WriteFile(forbidden, []byte("synthetic private content"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(forbidden); err != nil {
		t.Fatal("negative control is not actually accessible to parent", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal("network negative lacks a reachable positive control", err)
	}
	connection.Close()
	spec.Args = append(spec.Args, forbidden, listener.Addr().String())
	t.Setenv("ANTHROPIC_API_KEY", "synthetic-private-parent")
	t.Setenv("FUSION_PARENT_SECRET", "synthetic-other-secret")
	result, err := Run(context.Background(), artifact, private(t), spec)
	if err != nil || Evaluate(result, artifact, spec).Status != Passed {
		t.Fatal(err, result.Record())
	}
}

func TestRunnerStopsActualTimeoutAndSourceRevocation(t *testing.T) {
	for _, mode := range []string{"timeout", "revoke", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			artifact, spec, source := fixture(t, "wait")
			spec.Timeout = 200 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode != "timeout" {
				spec.Timeout = 2 * time.Second
			}
			if mode == "cancel" {
				go func() { time.Sleep(150 * time.Millisecond); cancel() }()
			}
			if mode == "revoke" {
				go func() {
					time.Sleep(150 * time.Millisecond)
					_ = os.WriteFile(filepath.Join(source, "tests/input.txt"), []byte("revoked"), 0600)
				}()
			}
			result, err := Run(ctx, artifact, private(t), spec)
			if err != nil || !result.Record().Executed || !result.Record().Stopped || !result.Record().Interrupted || Evaluate(result, artifact, spec).Status == Passed {
				t.Fatal(err, result.Record())
			}
		})
	}
}

func TestRunnerZeroTestsRequireExplicitFrozenProjectRule(t *testing.T) {
	artifact, spec, _ := fixture(t, "zero")
	spec.Rules = Rules{AllowZero: true}
	result, err := Run(context.Background(), artifact, private(t), spec)
	if err != nil || Evaluate(result, artifact, spec).Status != Passed {
		t.Fatal(err, result.Record())
	}
	spec.Rules = Rules{MinTests: 1}
	if Evaluate(result, artifact, spec).Status != Superseded {
		t.Fatal("changed zero-test standard reused old evidence")
	}
}

func TestRunnerRejectsBeforeExecutionMissingToolsVersionsAndSuite(t *testing.T) {
	for _, mode := range []string{"hash", "missing", "version", "suite", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			artifact, spec, _ := fixture(t, "passed")
			switch mode {
			case "hash":
				spec.Tool.SHA256 = strings.Repeat("0", 64)
			case "missing":
				spec.Tool.Executable += "-absent"
			case "version":
				spec.Tool.Version = "forged version"
			case "suite":
				spec.SuitePaths = []string{"absent"}
			case "timeout":
				spec.Timeout = 0
			}
			result, err := Run(context.Background(), artifact, private(t), spec)
			if err == nil || result.Record().Executed || Evaluate(result, artifact, spec).Status == Passed {
				t.Fatal(err, result.Record())
			}
		})
	}
}
