package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/fusion/workspace"
)

// GoToolchain is frozen by the trusted host. No caller environment, executable
// flags, external module cache or child permissions are imported.
type GoToolchain struct {
	Root     string `json:"root"`
	RootHash string `json:"root_hash"`
	Package  string `json:"package"`
	Vendor   bool   `json:"vendor"`
}
type GoCommand struct {
	Executable  string    `json:"executable"`
	ImageHash   string    `json:"image_hash"`
	Args        []string  `json:"args"`
	Environment []string  `json:"environment"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	ExitCode    int       `json:"exit_code"`
	Executed    bool      `json:"executed"`
	Stopped     bool      `json:"stopped"`
	Interrupted bool      `json:"interrupted"`
	Truncated   bool      `json:"truncated"`
	StdoutHash  string    `json:"stdout_hash"`
	StderrHash  string    `json:"stderr_hash"`
}

func cloneGoCommands(in []GoCommand) []GoCommand {
	if in == nil {
		return nil
	}
	out := append([]GoCommand(nil), in...)
	for i := range out {
		out[i].Args = append([]string(nil), out[i].Args...)
		out[i].Environment = append([]string(nil), out[i].Environment...)
	}
	return out
}
func validGoSpec(s Spec) bool {
	if s.Go == nil {
		return true
	}
	g := s.Go
	pkg := g.Package == "." || strings.HasPrefix(g.Package, "./") && workspace.ValidWritePaths([]string{strings.TrimPrefix(g.Package, "./")}) && !strings.Contains(g.Package, "...")
	return pkg && filepath.IsAbs(g.Root) && filepath.Clean(g.Root) == g.Root && len(g.RootHash) == 64 && s.Tool.Executable == filepath.Join(g.Root, "bin/go") && s.Tool.Version == "go version go1.26.3 darwin/arm64" && reflect.DeepEqual(s.Tool.VersionArgs, []string{"version"}) && reflect.DeepEqual(s.Args, goLogicalArgs(g))
}

func goLogicalArgs(g *GoToolchain) []string {
	mode := "readonly"
	if g.Vendor {
		mode = "vendor"
	}
	return []string{"test", "-count=1", "-json", "-vet=off", "-p=2", "-buildvcs=false", "-pgo=off", "-mod=" + mode, g.Package}
}

// TestsExecuted distinguishes a real test invocation from compilation failure.
// It is data derived from an owned receipt, not an import or execution grant.
func TestsExecuted(r Record) bool {
	if r.Spec.Go == nil {
		return r.Executed
	}
	return len(r.GoCommands) >= 4 && r.GoCommands[3].Executed
}

type sdkEntry struct {
	Path  string
	Size  int64
	Mode  uint32
	Mtime int64
	Hash  string
}

// GoRootHash pins the complete explicitly approved SDK, including compiler,
// assembler, linker, standard source and official test2json source. No symlinks.
func GoRootHash(root string) (string, error) {
	_, hash, err := sdkSnapshot(root, true)
	return hash, err
}
func sdkSnapshot(root string, contents bool) ([]sdkEntry, string, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != root {
		return nil, "", ErrInvalid
	}
	var entries []sdkEntry
	var total int64
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		i, e := d.Info()
		if e != nil || i.Mode().Perm()&0022 != 0 || i.Mode()&os.ModeSymlink != 0 {
			return ErrInvalid
		}
		if i.IsDir() {
			return nil
		}
		if !i.Mode().IsRegular() {
			return ErrInvalid
		}
		total += i.Size()
		if total > 512<<20 || len(entries) >= 20000 {
			return ErrInvalid
		}
		rel, _ := filepath.Rel(root, p)
		v := sdkEntry{Path: rel, Size: i.Size(), Mode: uint32(i.Mode()), Mtime: i.ModTime().UnixNano()}
		if contents {
			f, e := os.Open(p)
			if e != nil {
				return e
			}
			b, e := io.ReadAll(io.LimitReader(f, 256<<20+1))
			opened, statErr := f.Stat()
			f.Close()
			after, afterErr := os.Lstat(p)
			if e != nil || statErr != nil || afterErr != nil || !os.SameFile(i, opened) || !os.SameFile(i, after) || i.ModTime() != after.ModTime() || i.Size() != int64(len(b)) {
				return ErrInvalid
			}
			v.Hash = digest(b)
		}
		entries = append(entries, v)
		return nil
	})
	if err != nil || len(entries) == 0 {
		return nil, "", ErrInvalid
	}
	if !contents {
		return entries, "", nil
	}
	// Contents define the frozen SDK; mtime is only a running-process guard.
	stable := append([]sdkEntry(nil), entries...)
	for i := range stable {
		stable[i].Mtime = 0
	}
	return entries, jsonHash(stable), nil
}
func goRootCurrent(g *GoToolchain) bool {
	h, e := GoRootHash(g.Root)
	return e == nil && h == g.RootHash
}
func sdkMetadata(entries []sdkEntry) []sdkEntry {
	v := append([]sdkEntry(nil), entries...)
	for i := range v {
		v[i].Hash = ""
	}
	return v
}
func commandRecord(exe string, args, env []string, out execution) GoCommand {
	b, _ := os.ReadFile(exe)
	return GoCommand{exe, digest(b), append([]string(nil), args...), append([]string(nil), env...), out.started, out.finished, out.exit, out.executed, out.stopped, out.interrupted, out.truncated, digest(out.stdout), digest(out.stderr)}
}
func validGoCommands(r Record) bool {
	if r.Spec.Go == nil {
		return len(r.GoCommands) == 0
	}
	if len(r.GoCommands) < 2 || len(r.GoCommands) > 5 {
		return false
	}
	for i, c := range r.GoCommands {
		if !filepath.IsAbs(c.Executable) || !validArgs(c.Args) || len(c.Environment) < 9 || len(c.Environment) > 32 || c.StartedAt.IsZero() || c.FinishedAt.Before(c.StartedAt) || !c.Executed || !c.Stopped || len(c.ImageHash) != 64 || len(c.StdoutHash) != 64 || len(c.StderrHash) != 64 {
			return false
		}
		if i > 0 && c.StartedAt.Before(r.GoCommands[i-1].FinishedAt) {
			return false
		}
		if i < 3 && (c.Executable != r.Spec.Tool.Executable || c.ImageHash != r.Spec.Tool.SHA256) {
			return false
		}
	}
	first, last := r.GoCommands[0], r.GoCommands[len(r.GoCommands)-1]
	if !reflect.DeepEqual(first.Args, r.Spec.Tool.VersionArgs) || first.ExitCode != 0 || first.Interrupted || first.Truncated {
		return false
	}
	if !reflect.DeepEqual(last.Environment, r.Environment) {
		return false
	}
	build := r.GoCommands[1]
	if len(build.Args) != 10 || !filepath.IsAbs(build.Args[8]) || filepath.Clean(build.Args[8]) != build.Args[8] || filepath.Base(build.Args[8]) != "package.test" {
		return false
	}
	mode := "readonly"
	if r.Spec.Go.Vendor {
		mode = "vendor"
	}
	if !reflect.DeepEqual(build.Args, []string{"test", "-c", "-p=2", "-vet=off", "-buildvcs=false", "-pgo=off", "-mod=" + mode, "-o", build.Args[8], r.Spec.Go.Package}) {
		return false
	}
	converter := filepath.Join(filepath.Dir(build.Args[8]), "test2json")
	if len(r.GoCommands) > 2 {
		if build.ExitCode != 0 || build.Interrupted || build.Truncated || !reflect.DeepEqual(r.GoCommands[2].Args, []string{"build", "-p=2", "-buildvcs=false", "-pgo=off", "-o", converter, "cmd/test2json"}) || !reflect.DeepEqual(r.GoCommands[2].Environment, build.Environment) {
			return false
		}
	}
	if len(r.GoCommands) > 3 {
		c := r.GoCommands[2]
		if c.ExitCode != 0 || c.Interrupted || c.Truncated || r.GoCommands[3].Executable != build.Args[8] || !reflect.DeepEqual(r.GoCommands[3].Args, []string{"-test.v=test2json", "-test.count=1"}) || !reflect.DeepEqual(r.GoCommands[3].Environment, first.Environment) {
			return false
		}
	}
	if len(r.GoCommands) > 4 && (r.GoCommands[4].Executable != converter || !reflect.DeepEqual(r.GoCommands[4].Args, []string{"-p", r.Spec.Go.Package}) || !reflect.DeepEqual(r.GoCommands[4].Environment, first.Environment)) {
		return false
	}
	if r.ExitCode == 0 && !r.Interrupted && !r.Truncated {
		if len(r.GoCommands) != 5 {
			return false
		}
		for _, c := range r.GoCommands {
			if c.ExitCode != 0 || c.Interrupted || c.Truncated {
				return false
			}
		}
		if last.StdoutHash != r.ReportHash {
			return false
		}
	}
	return r.Executed && r.Stopped && r.StartedAt.Equal(first.StartedAt) && r.FinishedAt.Equal(last.FinishedAt)
}

func runGo(ctx context.Context, a workspace.FrozenArtifact, root string, s Spec) (Result, error) {
	if ctx == nil || ctx.Err() != nil || !available() || !validSpec(s) || !toolCurrent(s.Tool) || workspace.PrivateState(root) != nil {
		return Result{}, ErrInvalid
	}
	suite, ok := suiteHash(a, s.SuitePaths)
	if !ok {
		return Result{}, ErrInvalid
	}
	entries, hash, err := sdkSnapshot(s.Go.Root, true)
	if err != nil || hash != s.Go.RootHash {
		return Result{}, ErrInvalid
	}
	metadata := sdkMetadata(entries)
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return Result{}, ErrInvalid
	}
	runRoot, err := os.MkdirTemp(root, "go-verify-")
	if err != nil {
		return Result{}, ErrInvalid
	}
	input, err := a.Copy(runRoot, "input")
	if err != nil {
		return Result{}, ErrInvalid
	}
	makeScratch := func(name string) (string, error) {
		p := filepath.Join(runRoot, name)
		if e := os.Mkdir(p, 0700); e != nil {
			return "", e
		}
		for _, folder := range []string{"home", "config", "cache", "data", "tmp", "modules"} {
			if e := os.Mkdir(filepath.Join(p, folder), 0700); e != nil {
				return "", e
			}
		}
		return p, nil
	}
	buildScratch, err := makeScratch("build")
	if err != nil {
		return Result{}, ErrInvalid
	}
	testScratch, err := makeScratch("test")
	if err != nil {
		return Result{}, ErrInvalid
	}
	current := func() bool {
		i, e := os.Lstat(root)
		m, _, metaErr := sdkSnapshot(s.Go.Root, false)
		return e == nil && os.SameFile(rootInfo, i) && workspace.PrivateState(root) == nil && a.Current() && input.SourceCurrent() && toolCurrent(s.Tool) && metaErr == nil && reflect.DeepEqual(metadata, m)
	}
	ownedCtx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	versionProfile, versionEnv := sandbox(s.Tool.Executable, input.Path, testScratch)
	version := execute(ownedCtx, s.Tool.Executable, s.Tool.VersionArgs, input.Path, versionProfile, versionEnv, 4096, current)
	if !version.stopped || version.interrupted || version.truncated || version.exit != 0 || strings.TrimSpace(string(version.stdout)) != s.Tool.Version || !current() {
		return Result{}, ErrInvalid
	}
	commands := []GoCommand{commandRecord(s.Tool.Executable, s.Tool.VersionArgs, versionEnv, version)}
	profile, env := goCompilerSandbox(s, input.Path, buildScratch)
	mode := "readonly"
	if s.Go.Vendor {
		mode = "vendor"
	}
	testBin := filepath.Join(buildScratch, "package.test")
	converter := filepath.Join(buildScratch, "test2json")
	compilation := [][]string{{"test", "-c", "-p=2", "-vet=off", "-buildvcs=false", "-pgo=off", "-mod=" + mode, "-o", testBin, s.Go.Package}, {"build", "-p=2", "-buildvcs=false", "-pgo=off", "-o", converter, "cmd/test2json"}}
	var last execution
	var report, stderr []byte
	for _, args := range compilation {
		last = executeOwned(ownedCtx, s.Tool.Executable, args, input.Path, profile, env, 1<<20, current, nil, true)
		commands = append(commands, commandRecord(s.Tool.Executable, args, env, last))
		stderr = append(stderr, last.stderr...)
		if !last.stopped {
			return Result{}, ErrInvalid
		}
		if last.exit != 0 || last.interrupted || last.truncated {
			report = last.stdout
			return goResult(a, s, input, runRoot, current, suite, env, commands, last, report, stderr), nil
		}
	}
	// The compiler sandbox cannot execute either generated binary. Project code
	// gets a fresh scratch; it cannot modify compiler outputs or spawn children.
	testData, err := os.ReadFile(testBin)
	if err != nil {
		return Result{}, ErrInvalid
	}
	converterData, err := os.ReadFile(converter)
	if err != nil {
		return Result{}, ErrInvalid
	}
	binaryCurrent := func() bool {
		return current() && toolCurrent(Tool{Executable: testBin, SHA256: digest(testData)}) && toolCurrent(Tool{Executable: converter, SHA256: digest(converterData)})
	}
	testProfile, testEnv := sandbox(testBin, input.Path, testScratch)
	testDir := input.Path
	if s.Go.Package != "." {
		testDir = filepath.Join(input.Path, strings.TrimPrefix(s.Go.Package, "./"))
	}
	args := []string{"-test.v=test2json", "-test.count=1"}
	last = execute(ownedCtx, testBin, args, testDir, testProfile, testEnv, 1<<20, binaryCurrent)
	commands = append(commands, commandRecord(testBin, args, testEnv, last))
	stderr = append(stderr, last.stderr...)
	if !last.stopped {
		return Result{}, ErrInvalid
	}
	testExit := last.exit
	interrupted, truncated := last.interrupted, last.truncated
	convertProfile, convertEnv := sandbox(converter, input.Path, testScratch)
	convertArgs := []string{"-p", s.Go.Package}
	converted := executeOwned(ownedCtx, converter, convertArgs, input.Path, convertProfile, convertEnv, 1<<20, binaryCurrent, last.stdout, false)
	commands = append(commands, commandRecord(converter, convertArgs, convertEnv, converted))
	stderr = append(stderr, converted.stderr...)
	if !converted.stopped {
		return Result{}, ErrInvalid
	}
	if testExit != 0 {
		converted.exit = testExit
	}
	converted.interrupted = converted.interrupted || interrupted
	converted.truncated = converted.truncated || truncated
	return goResult(a, s, input, runRoot, binaryCurrent, suite, testEnv, commands, converted, converted.stdout, stderr), nil
}
func goResult(a workspace.FrozenArtifact, s Spec, input workspace.Snapshot, root string, current func() bool, suite string, env []string, commands []GoCommand, last execution, report, stderr []byte) Result {
	rec := Record{Version: 1, Spec: clone(s), Environment: append([]string(nil), env...), ArtifactHash: a.Manifest().TreeHash, SuiteHash: suite, SpecHash: jsonHash(s), EnvironmentHash: jsonHash(env), ToolVersion: s.Tool.Version, ReportHash: digest(report), StderrHash: digest(stderr), StartedAt: commands[0].StartedAt, FinishedAt: commands[len(commands)-1].FinishedAt, ExitCode: last.exit, Executed: last.executed, Stopped: last.stopped, Interrupted: last.interrupted, Truncated: last.truncated, GoCommands: cloneGoCommands(commands)}
	if len(stderr) > 1<<20 {
		rec.Truncated = true
		stderr = stderr[:1<<20]
		rec.StderrHash = digest(stderr)
	}
	if !current() || !goRootCurrent(s.Go) {
		rec.InputChanged = true
	} else {
		post, e := workspace.Freeze(input, root, "post-input")
		rec.InputChanged = e != nil || post.Manifest().TreeHash != rec.ArtifactHash
	}
	return Result{owned: &receipt{rec, append([]byte(nil), report...), append([]byte(nil), stderr...), a, clone(s)}}
}

// Parse the actual official converter stream, not ad-hoc report text. Require
// one start, balanced named cases and exactly one final package outcome.
func parseGoReport(data []byte, pkg string) (counts, error) {
	var c counts
	type event struct {
		Action  string
		Package string
		Test    string
		Output  string
		Elapsed float64
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	states := map[string]string{}
	started, finished := false, false
	for {
		var e event
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil || token != json.Delim('{') {
			return counts{}, ErrInvalid
		}
		fields := map[string]json.RawMessage{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return counts{}, ErrInvalid
			}
			name, ok := key.(string)
			if !ok {
				return counts{}, ErrInvalid
			}
			if _, exists := fields[name]; exists {
				return counts{}, ErrInvalid
			}
			var value json.RawMessage
			if decoder.Decode(&value) != nil {
				return counts{}, ErrInvalid
			}
			fields[name] = value
		}
		if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
			return counts{}, ErrInvalid
		}
		raw, err := json.Marshal(fields)
		if err != nil {
			return counts{}, ErrInvalid
		}
		one := json.NewDecoder(strings.NewReader(string(raw)))
		one.DisallowUnknownFields()
		err = one.Decode(&e)
		if err == io.EOF {
			break
		}
		if err != nil || e.Package != pkg || finished {
			return counts{}, errors.New("invalid Go test events")
		}
		if e.Elapsed < 0 || math.IsNaN(e.Elapsed) || math.IsInf(e.Elapsed, 0) {
			return counts{}, ErrInvalid
		}
		if e.Action == "start" {
			if started || e.Test != "" {
				return counts{}, ErrInvalid
			}
			started = true
			continue
		}
		if !started {
			return counts{}, ErrInvalid
		}
		if e.Action == "output" {
			continue
		}
		if e.Test == "" {
			if e.Action != "pass" && e.Action != "fail" && e.Action != "skip" {
				return counts{}, ErrInvalid
			}
			for _, v := range states {
				if v != "done" {
					return counts{}, ErrInvalid
				}
			}
			finished = true
			if e.Action == "fail" {
				c.failures++
			}
			continue
		}
		if e.Action == "run" {
			if _, exists := states[e.Test]; exists {
				return counts{}, ErrInvalid
			}
			states[e.Test] = "running"
			continue
		}
		state, exists := states[e.Test]
		if !exists {
			return counts{}, ErrInvalid
		}
		switch e.Action {
		case "pause":
			if state != "running" {
				return counts{}, ErrInvalid
			}
			states[e.Test] = "paused"
		case "cont":
			if state != "paused" {
				return counts{}, ErrInvalid
			}
			states[e.Test] = "running"
		case "pass", "fail", "skip":
			if state != "running" {
				return counts{}, ErrInvalid
			}
			states[e.Test] = "done"
			c.tests++
			if e.Action == "fail" {
				c.failures++
			}
			if e.Action == "skip" {
				c.skipped++
			}
		default:
			return counts{}, ErrInvalid
		}
	}
	if !started || !finished {
		return counts{}, ErrInvalid
	}
	return c, nil
}
