package evidence

import (
	"path/filepath"
	"reflect"

	"github.com/yetone/magpie/internal/fusion/workspace"
)

// Stored is exported data, not an owned Result or a grant. Only a protected
// Store receipt written from Export may be fed to EvaluateStored by the host.
type Stored struct {
	Record       Record  `json:"record"`
	Report       []byte  `json:"report"`
	Stderr       []byte  `json:"stderr"`
	Verdict      Verdict `json:"verdict"`
	ArtifactPath string  `json:"artifact_path"`
}

func ValidateSpec(s Spec) error {
	if !available() || !validSpec(s) || !toolCurrent(s.Tool) || s.Go != nil && !goRootCurrent(s.Go) {
		return ErrInvalid
	}
	return nil
}

func Export(r Result, a workspace.FrozenArtifact, s Spec) (Stored, error) {
	if r.owned == nil || !a.Current() || a.Path() != r.owned.artifact.Path() || !reflect.DeepEqual(clone(s), r.owned.spec) {
		return Stored{}, ErrInvalid
	}
	v := Evaluate(r, a, s)
	if v.Status == Superseded || !r.Record().Executed || !r.Record().Stopped {
		return Stored{}, ErrInvalid
	}
	report, stderr := r.Logs()
	out := Stored{r.Record(), report, stderr, v, a.Path()}
	if !ValidStored(out) {
		return Stored{}, ErrInvalid
	}
	return out, nil
}

func ValidStored(v Stored) bool {
	r := v.Record
	if r.Version != 1 || !validSpec(r.Spec) || !filepath.IsAbs(v.ArtifactPath) || filepath.Clean(v.ArtifactPath) != v.ArtifactPath || r.SpecHash != jsonHash(clone(r.Spec)) || r.ToolVersion != r.Spec.Tool.Version || !validGoCommands(r) || r.EnvironmentHash != jsonHash(r.Environment) || (r.Spec.Go == nil && len(r.Environment) != 9 || r.Spec.Go != nil && (len(r.Environment) < 9 || len(r.Environment) > 32)) || r.StartedAt.IsZero() || r.FinishedAt.Before(r.StartedAt) || !r.Executed || !r.Stopped || len(v.Report) > 1<<20 || len(v.Stderr) > 1<<20 || r.ReportHash != digest(v.Report) || r.StderrHash != digest(v.Stderr) {
		return false
	}
	for _, h := range []string{r.ArtifactHash, r.SuiteHash, r.SpecHash, r.EnvironmentHash, r.ReportHash, r.StderrHash} {
		if len(h) != 64 {
			return false
		}
		for _, c := range h {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
	}
	want := evaluateReport(r, v.Report, r.Spec.Rules)
	return reflect.DeepEqual(want, v.Verdict)
}

// EvaluateStored checks data integrity and current inputs. It cannot establish
// report origin by itself; callers must first obtain the private Store receipt
// for the exact released Task/Plan/Run and independently restore its artifact.
func EvaluateStored(v Stored, a workspace.FrozenArtifact, s Spec) Verdict {
	if !ValidStored(v) {
		return Verdict{Status: Unverified, Reason: "damaged_stored_verification"}
	}
	suite, ok := suiteHash(a, s.SuitePaths)
	if !ok || a.Path() != v.ArtifactPath || a.Manifest().TreeHash != v.Record.ArtifactHash || suite != v.Record.SuiteHash || !reflect.DeepEqual(clone(s), v.Record.Spec) || v.Record.InputChanged {
		return Verdict{Status: Superseded, Reason: "artifact_suite_or_standard_changed"}
	}
	if s.Go != nil && !goRootCurrent(s.Go) {
		return Verdict{Status: Superseded, Reason: "toolchain_changed"}
	}
	return evaluateReport(v.Record, v.Report, s.Rules)
}

func evaluateReport(rec Record, report []byte, rules Rules) Verdict {
	if !rec.Executed || !rec.Stopped || rec.Interrupted {
		return Verdict{Status: Unverified, Reason: "execution_not_completed"}
	}
	if rec.ExitCode != 0 {
		return Verdict{Status: Failed, Reason: "nonzero_exit"}
	}
	if rec.Truncated {
		return Verdict{Status: Unverified, Reason: "truncated_report"}
	}
	c, err := parseJUnit(report)
	if rec.Spec.Go != nil {
		c, err = parseGoReport(report, rec.Spec.Go.Package)
	}
	if err != nil {
		return Verdict{Status: Unverified, Reason: "report_parse_failed"}
	}
	v := Verdict{Status: Passed, Tests: c.tests, Skipped: c.skipped}
	if c.failures+c.errors > 0 {
		v.Status = Failed
		v.Reason = "test_failure"
	} else if c.tests < c.skipped+rules.MinTests || c.tests == 0 && !rules.AllowZero || c.skipped > 0 && !rules.AllowSkipped {
		v.Status = Unverified
		v.Reason = "insufficient_executed_tests"
	}
	return v
}
