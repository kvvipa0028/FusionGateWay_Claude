//go:build darwin

package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/evidence"
	"github.com/yetone/magpie/internal/fusion/handoff"
)

func TestGLMVerificationWorker(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "version" {
		fmt.Print("glm-owned-verifier 1\n")
		os.Exit(0)
	}
	for _, name := range []string{"src/implemented.txt", "tests/generated.txt"} {
		b, err := os.ReadFile(name)
		if err != nil || string(b) != "synthetic factory file\n" {
			os.Exit(83)
		}
	}
	if mode == "malformed" {
		fmt.Print("<testsuite>")
		os.Exit(0)
	}
	fmt.Print(`<testsuite tests="2"><testcase name="actual_implementation"/><testcase name="actual_generated_test"/></testsuite>`)
	if mode == "fail" {
		os.Exit(7)
	}
	os.Exit(0)
}

func glmVerificationFixture(t *testing.T, mode string) *GLMVerificationConfig {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	return &GLMVerificationConfig{Acceptance: []string{"genuine tests required"}, Spec: evidence.Spec{Tool: evidence.Tool{Executable: exe, SHA256: handoff.Hash(raw), Version: "glm-owned-verifier 1", VersionArgs: []string{"-test.run=^TestGLMVerificationWorker$", "--", "version"}}, Args: []string{"-test.run=^TestGLMVerificationWorker$", "--", mode}, SuitePaths: []string{"tests"}, Rules: evidence.Rules{MinTests: 2}, Timeout: 2 * time.Second}}
}
