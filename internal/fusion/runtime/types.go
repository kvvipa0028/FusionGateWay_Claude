package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/store"
)

var (
	ErrUnsupported = errors.New("runtime capability unsupported")
	ErrLaunch      = errors.New("managed launch refused")
	ErrIdentity    = errors.New("process birth identity mismatch")
)

type Capabilities struct{ Probe, Start, Events, Cancel, Resume, ChildProcesses, Network bool }

// Adapter implementations must explicitly report unsupported capabilities.
// A successful process launch alone does not establish model/route admission.
type Adapter interface {
	Probe(context.Context) (Capabilities, error)
	Start(context.Context, store.StageRun, Spec) (*Handle, error)
	Resume(context.Context, store.StageRun) (*Handle, error)
}
type Spec struct {
	Executable, ExecutableHash string
	Args                       []string
	Root, Workspace            string
	Writable                   bool
	Timeout                    time.Duration
	NativeSessionID            string
	FixtureEnvironment         map[string]string
	Input                      []byte
	ValidateOutcome            func([]byte) bool
}
type Identity struct {
	PID         int   `json:"pid"`
	StartMicros int64 `json:"start_micros"`
}
type Event struct {
	RunID           string
	Generation, Seq int64
	Kind            string
}
type Result struct {
	State           string           `json:"state"`
	ExitCode        int              `json:"exit_code"`
	StoppedVerified bool             `json:"stopped_verified"`
	OutputHash      string           `json:"output_hash"`
	OutputBytes     int              `json:"output_bytes"`
	Proof           policy.StopProof `json:"proof"`
}

func FileHash(p string) (string, error) {
	f, e := os.Open(p)
	if e != nil {
		return "", ErrLaunch
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", ErrLaunch
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func hashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
