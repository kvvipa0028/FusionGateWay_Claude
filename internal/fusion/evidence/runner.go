// Package evidence executes trusted frozen commands on untrusted code. A JSON
// report or model response cannot construct the owned receipt used by Evaluate.
package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/workspace"
)

var ErrInvalid = errors.New("verification input unavailable or invalid")

type Tool struct {
	Executable  string   `json:"executable"`
	SHA256      string   `json:"sha256"`
	Version     string   `json:"version"`
	VersionArgs []string `json:"version_args"`
}
type Rules struct {
	MinTests     int  `json:"min_tests"`
	AllowZero    bool `json:"allow_zero"`
	AllowSkipped bool `json:"allow_skipped"`
}

// Spec is supplied by the trusted host, never by a model or an HTTP DTO.
// No arbitrary environment, network endpoint, writable path or shell is added.
type Spec struct {
	Tool       Tool          `json:"tool"`
	Args       []string      `json:"args"`
	SuitePaths []string      `json:"suite_paths"`
	Rules      Rules         `json:"rules"`
	Timeout    time.Duration `json:"timeout_ns"`
	Go         *GoToolchain  `json:"go_toolchain,omitempty"`
}
type Record struct {
	Version         int         `json:"version"`
	Spec            Spec        `json:"spec"`
	Environment     []string    `json:"environment"`
	ArtifactHash    string      `json:"artifact_hash"`
	SuiteHash       string      `json:"suite_hash"`
	SpecHash        string      `json:"spec_hash"`
	EnvironmentHash string      `json:"environment_hash"`
	ToolVersion     string      `json:"tool_version"`
	ReportHash      string      `json:"report_hash"`
	StderrHash      string      `json:"stderr_hash"`
	StartedAt       time.Time   `json:"started_at"`
	FinishedAt      time.Time   `json:"finished_at"`
	ExitCode        int         `json:"exit_code"`
	Executed        bool        `json:"executed"`
	Stopped         bool        `json:"stopped"`
	Truncated       bool        `json:"truncated"`
	Interrupted     bool        `json:"interrupted"`
	InputChanged    bool        `json:"input_changed"`
	GoCommands      []GoCommand `json:"go_commands,omitempty"`
}
type receipt struct {
	record         Record
	stdout, stderr []byte
	artifact       workspace.FrozenArtifact
	spec           Spec
}
type Result struct{ owned *receipt }

func (r Result) Record() Record {
	if r.owned == nil {
		return Record{}
	}
	v := r.owned.record
	v.Spec = clone(v.Spec)
	v.Environment = append([]string(nil), v.Environment...)
	v.GoCommands = cloneGoCommands(v.GoCommands)
	return v
}
func (r Result) Logs() (stdout, stderr []byte) {
	if r.owned != nil {
		return append([]byte(nil), r.owned.stdout...), append([]byte(nil), r.owned.stderr...)
	}
	return nil, nil
}
func (Result) String() string   { return "owned verification result (redacted)" }
func (Result) GoString() string { return "Result(<redacted>)" }

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func jsonHash(v any) string  { b, _ := json.Marshal(v); return digest(b) }
func clone(s Spec) Spec {
	s.Args = append([]string(nil), s.Args...)
	s.SuitePaths = append([]string(nil), s.SuitePaths...)
	s.Tool.VersionArgs = append([]string(nil), s.Tool.VersionArgs...)
	if s.Go != nil {
		v := *s.Go
		s.Go = &v
	}
	return s
}
func validArgs(args []string) bool {
	if len(args) > 128 {
		return false
	}
	for _, a := range args {
		if len(a) > 8192 || !utf8.ValidString(a) || strings.ContainsRune(a, 0) {
			return false
		}
	}
	return true
}
func validSpec(s Spec) bool {
	if !validGoSpec(s) {
		return false
	}
	return filepath.IsAbs(s.Tool.Executable) && filepath.Clean(s.Tool.Executable) == s.Tool.Executable && len(s.Tool.SHA256) == 64 && s.Tool.Version != "" && len(s.Tool.Version) <= 4096 && !strings.ContainsAny(s.Tool.Version, "\r\n\x00") && validArgs(s.Tool.VersionArgs) && len(s.Tool.VersionArgs) > 0 && validArgs(s.Args) && workspace.ValidWritePaths(s.SuitePaths) && len(s.SuitePaths) > 0 && s.Rules.MinTests >= 0 && s.Rules.MinTests <= 100000 && (s.Rules.MinTests > 0 || s.Rules.AllowZero) && s.Timeout > 0 && s.Timeout <= 10*time.Minute
}
func toolCurrent(tool Tool) bool {
	actual, e := filepath.EvalSymlinks(tool.Executable)
	if e != nil || actual != tool.Executable {
		return false
	}
	i, e := os.Lstat(actual)
	if e != nil || !i.Mode().IsRegular() || i.Mode().Perm()&0022 != 0 || i.Mode()&0100 == 0 || i.Size() > 256<<20 {
		return false
	}
	f, e := os.Open(actual)
	if e != nil {
		return false
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(i, opened) {
		return false
	}
	h := sha256.New()
	if _, e = io.Copy(h, io.LimitReader(f, 256<<20+1)); e != nil {
		return false
	}
	after, e := os.Lstat(actual)
	return e == nil && os.SameFile(i, after) && i.Size() == after.Size() && i.ModTime() == after.ModTime() && hex.EncodeToString(h.Sum(nil)) == tool.SHA256
}
func suiteHash(a workspace.FrozenArtifact, paths []string) (string, bool) {
	if !a.Current() || !workspace.ValidWritePaths(paths) || len(paths) == 0 {
		return "", false
	}
	var entries []workspace.ArtifactEntry
	matched := make([]bool, len(paths))
	for _, e := range a.Manifest().Entries {
		for i, p := range paths {
			if workspace.WritePathAllowed(e.Path, []string{p}) {
				matched[i] = true
			}
		}
		if workspace.WritePathAllowed(e.Path, paths) {
			entries = append(entries, e)
		}
	}
	for _, m := range matched {
		if !m {
			return "", false
		}
	}
	return jsonHash(entries), true
}

// Run supports a direct pinned executable on macOS. Child creation is denied;
// toolchains needing child processes require a separately admitted backend.
// The captured stdout is the report, so a pre-existing report cannot substitute
// for a process execution. The artifact and its copied input remain read-only.
func Run(ctx context.Context, a workspace.FrozenArtifact, root string, in Spec) (Result, error) {
	s := clone(in)
	if s.Go != nil {
		return runGo(ctx, a, root, s)
	}
	if ctx == nil || ctx.Err() != nil || !available() || !validSpec(s) || !toolCurrent(s.Tool) || workspace.PrivateState(root) != nil {
		return Result{}, ErrInvalid
	}
	suite, ok := suiteHash(a, s.SuitePaths)
	if !ok {
		return Result{}, ErrInvalid
	}
	rootInfo, e := os.Lstat(root)
	if e != nil {
		return Result{}, ErrInvalid
	}
	runRoot, e := os.MkdirTemp(root, "verify-")
	if e != nil {
		return Result{}, ErrInvalid
	}
	input, e := a.Copy(runRoot, "input")
	if e != nil {
		return Result{}, ErrInvalid
	}
	scratch := filepath.Join(runRoot, "scratch")
	if e = os.Mkdir(scratch, 0700); e != nil {
		return Result{}, ErrInvalid
	}
	for _, p := range []string{"home", "config", "cache", "data", "tmp"} {
		if os.Mkdir(filepath.Join(scratch, p), 0700) != nil {
			return Result{}, ErrInvalid
		}
	}
	guard := func() bool {
		actual, e := os.Lstat(root)
		return e == nil && os.SameFile(rootInfo, actual) && workspace.PrivateState(root) == nil && a.Current() && input.SourceCurrent() && toolCurrent(s.Tool)
	}
	profile, env := sandbox(s.Tool.Executable, input.Path, scratch)
	// Every process uses only this deterministic private environment. Both the
	// frozen command and the exact actual environment are recorded independently.
	ownedCtx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	version := execute(ownedCtx, s.Tool.Executable, s.Tool.VersionArgs, input.Path, profile, env, 4096, guard)
	if !version.stopped || version.interrupted || version.truncated || version.exit != 0 || strings.TrimSpace(string(version.stdout)) != s.Tool.Version || !guard() {
		return Result{}, ErrInvalid
	}
	out := execute(ownedCtx, s.Tool.Executable, s.Args, input.Path, profile, env, 1<<20, guard)
	rec := Record{Version: 1, Spec: clone(s), Environment: append([]string(nil), env...), ArtifactHash: a.Manifest().TreeHash, SuiteHash: suite, SpecHash: jsonHash(s), EnvironmentHash: jsonHash(env), ToolVersion: strings.TrimSpace(string(version.stdout)), ReportHash: digest(out.stdout), StderrHash: digest(out.stderr), StartedAt: out.started, FinishedAt: out.finished, ExitCode: out.exit, Executed: out.executed, Stopped: out.stopped, Truncated: out.truncated, Interrupted: out.interrupted}
	if !guard() {
		rec.InputChanged = true
	} else {
		final, e := workspace.Freeze(input, runRoot, "post-input")
		rec.InputChanged = e != nil || final.Manifest().TreeHash != rec.ArtifactHash
	}
	return Result{owned: &receipt{rec, append([]byte(nil), out.stdout...), append([]byte(nil), out.stderr...), a, s}}, nil
}

type Status string

const (
	Passed     Status = "passed"
	Failed     Status = "failed"
	Unverified Status = "unverified"
	Superseded Status = "superseded"
)

type Verdict struct {
	Status  Status `json:"status"`
	Reason  string `json:"reason"`
	Tests   int    `json:"tests"`
	Skipped int    `json:"skipped"`
}

func Evaluate(r Result, current workspace.FrozenArtifact, s Spec) Verdict {
	if r.owned == nil {
		return Verdict{Status: Unverified, Reason: "no_owned_execution"}
	}
	v := r.owned
	rec := v.record
	suite, ok := suiteHash(current, s.SuitePaths)
	if !ok || current.Path() != v.artifact.Path() || !v.artifact.Current() || rec.InputChanged || current.Manifest().TreeHash != rec.ArtifactHash || suite != rec.SuiteHash || !reflect.DeepEqual(clone(s), v.spec) {
		return Verdict{Status: Superseded, Reason: "artifact_suite_or_standard_changed"}
	}
	if s.Go != nil && !goRootCurrent(s.Go) {
		return Verdict{Status: Superseded, Reason: "toolchain_changed"}
	}
	return evaluateReport(rec, v.stdout, s.Rules)
}

type bounded struct {
	mu        sync.Mutex
	data      []byte
	limit     int
	truncated bool
}

func (b *bounded) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := b.limit - len(b.data)
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	b.data = append(b.data, p...)
	return n, nil
}

type execution struct {
	stdout, stderr                            []byte
	exit                                      int
	executed, stopped, truncated, interrupted bool
	started, finished                         time.Time
}
