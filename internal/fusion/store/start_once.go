package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// StartIdentity describes the client's explicit stage/plan/generation request.
// Owner/TTL/admission observations belong to the controller and are not part
// of this request. Target is frozen in the referenced run, never an HTTP field.
type StartIdentity struct {
	TaskID       string           `json:"task_id"`
	Role         stageplan.Role   `json:"role"`
	PlanRevision int64            `json:"plan_revision"`
	Generation   int64            `json:"generation"`
	Restore      *RestoreIdentity `json:"restore,omitempty"`
}

// RestoreIdentity binds a retry to a specific origin and sealed checkpoint.
// It is request identity, not a seal, stop proof or execution permission.
type RestoreIdentity struct {
	OriginRunID      string `json:"origin_run_id"`
	CheckpointID     string `json:"checkpoint_id"`
	CheckpointDigest string `json:"checkpoint_digest"`
}

func validRestoreIdentity(in *RestoreIdentity) bool {
	if in == nil {
		return true
	}
	if !opaque(in.OriginRunID) {
		return false
	}
	for _, value := range []string{in.CheckpointID, in.CheckpointDigest} {
		if len(value) != 64 {
			return false
		}
		for _, c := range value {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
	}
	return true
}

// Only Created=true authorizes attempting to launch this intent. A retry
// returns current persisted state (including terminal/unknown), not permission
// to invoke Runtime again. Unknown still requires explicit reconciliation.
type StartReceipt struct {
	Run     StageRun
	Created bool
}

func startIdentity(in StartRequest) StartIdentity {
	gen := int64(-1)
	if in.ExpectedGeneration != nil {
		gen = *in.ExpectedGeneration
	}
	return StartIdentity{TaskID: in.TaskID, Role: in.Role, PlanRevision: in.PlanRevision, Generation: gen, Restore: in.Restore}
}
func validStartIdentity(in StartIdentity) bool {
	if !opaque(in.TaskID) || in.PlanRevision < 1 || in.Generation < 0 || !validRestoreIdentity(in.Restore) {
		return false
	}
	for _, role := range stageplan.AllRoles() {
		if role == in.Role {
			return true
		}
	}
	return false
}
func startHash(in StartIdentity) string { raw, _ := json.Marshal(in); return hash(raw) }
func lookupStartIn(q queryRow, key string, in StartIdentity) (StageRun, error) {
	if !opaque(key) || !validStartIdentity(in) {
		return StageRun{}, ErrInvalid
	}
	var payload, runID string
	e := q.QueryRow("SELECT payload_hash,run_id FROM start_requests WHERE key=?", key).Scan(&payload, &runID)
	if errors.Is(e, sql.ErrNoRows) {
		return StageRun{}, ErrNotFound
	}
	if e != nil {
		return StageRun{}, e
	}
	if payload != startHash(in) {
		return StageRun{}, ErrConflict
	}
	r, e := runIn(q, runID)
	if e != nil {
		return StageRun{}, e
	}
	if r.TaskID != in.TaskID || r.Role != in.Role || r.PlanRevision != in.PlanRevision {
		return StageRun{}, ErrConflict
	}
	return r, nil
}

// LookupStart is a read-only idempotent retry/reconnect path. It does not
// refresh leases, grant capabilities, replay work or update admission evidence.
func (s *Store) LookupStart(key string, in StartIdentity) (StageRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return StageRun{}, ErrClosed
	}
	return lookupStartIn(s.db, key, in)
}

// StartReservedOnce adds the request mapping in the same durable transaction
// as generation/attempt, startup intent, budget checks and reservation. A
// repeated key is read before capacity/budget checks and cannot consume them.
func (s *Store) StartReservedOnce(in StartRequest, req ReservationRequest) (StartReceipt, error) {
	if !opaque(in.IdempotencyKey) || in.ExpectedGeneration == nil || !validStartIdentity(startIdentity(in)) {
		return StartReceipt{}, ErrInvalid
	}
	return s.startReserved(in, req)
}
