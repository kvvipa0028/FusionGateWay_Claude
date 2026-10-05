package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/task"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

//go:embed migrations/001.sql
var migration string

//go:embed migrations/002.sql
var migrationTwo string

//go:embed migrations/003.sql
var migrationThree string

//go:embed migrations/004.sql
var migrationFour string

//go:embed migrations/005.sql
var migrationFive string

//go:embed migrations/006.sql
var migrationSix string

//go:embed migrations/007.sql
var migrationSeven string

//go:embed migrations/008.sql
var migrationEight string

//go:embed migrations/009.sql
var migrationNine string

//go:embed migrations/010.sql
var migrationTen string

//go:embed migrations/011.sql
var migrationEleven string

//go:embed migrations/012.sql
var migrationTwelve string
var (
	ErrConflict         = errors.New("store conflict")
	ErrFenced           = errors.New("execution fenced")
	ErrInvalid          = errors.New("invalid store input")
	ErrControllerActive = errors.New("controller already active")
	ErrUnsupported      = errors.New("unsupported store platform or schema")
	ErrNotFound         = errors.New("record not found")
	ErrClosed           = errors.New("store closed")
)

type Task = task.Task
type StageRun = task.StageRun
type Event = task.Event
type EvidenceRef = task.EvidenceRef
type CreateRequest struct {
	ProjectID string             `json:"project_id"`
	Goal      string             `json:"goal"`
	Plan      stageplan.Snapshot `json:"plan"`
	Budget    *Budget            `json:"budget,omitempty"`
	Preset    *PresetRef         `json:"preset,omitempty"`
}
type StartRequest struct {
	TaskID             string
	Role               stageplan.Role
	PlanRevision       int64
	Owner              string
	TTL                time.Duration
	Target             stageplan.ExecutionTarget
	IdempotencyKey     string
	ExpectedGeneration *int64
	Restore            *RestoreIdentity
}
type Store struct {
	mu   sync.Mutex
	db   *sql.DB
	lock *os.File
	now  func() time.Time
}

// An observed lease expiry must commit its fence even though the worker call
// fails. Exhausted stage intents have their own private fence sentinel.
// Other transaction errors always roll back.
var errExpiredFence = errors.New("commit observed lease expiry")

func opaque(v string) bool {
	return v != "" && len(v) <= 256 && !strings.ContainsFunc(v, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) })
}
func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func id(prefix string) (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return prefix + hex.EncodeToString(b[:]), nil
}
func Open(root string) (*Store, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, ErrInvalid
	}
	canonical, e := filepath.EvalSymlinks(root)
	if e != nil || canonical != root {
		return nil, ErrInvalid
	}
	info, e := os.Lstat(root)
	if e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 || !privateOwner(info) {
		return nil, ErrInvalid
	}
	for current := root; ; current = filepath.Dir(current) {
		if _, e = os.Lstat(filepath.Join(current, ".git")); e == nil || !os.IsNotExist(e) {
			return nil, ErrInvalid
		}
		if current == filepath.Dir(current) {
			break
		}
	}
	lock, e := lockController(filepath.Join(root, "fusion.lock"))
	if e != nil {
		return nil, e
	}
	s := &Store{lock: lock, now: time.Now}
	fail := func(e error) (*Store, error) { s.Close(); return nil, e }
	dbpath := filepath.Join(root, "fusion.db")
	if info, e = os.Lstat(dbpath); os.IsNotExist(e) {
		f, e := os.OpenFile(dbpath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return fail(e)
		}
		f.Close()
	} else if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !privateOwner(info) {
		return fail(ErrInvalid)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if info, e = os.Lstat(dbpath + suffix); e == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !privateOwner(info)) {
			return fail(ErrInvalid)
		} else if e != nil && !os.IsNotExist(e) {
			return fail(ErrInvalid)
		}
	}
	s.db, e = sql.Open("sqlite", dbpath)
	if e != nil {
		return fail(e)
	}
	s.db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL"} {
		if _, e = s.db.Exec(pragma); e != nil {
			return fail(e)
		}
	}
	var version int
	if e = s.db.QueryRow("PRAGMA user_version").Scan(&version); e != nil {
		return fail(e)
	}
	if version == 0 {
		var count int
		if e = s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table'").Scan(&count); e != nil || count != 0 {
			return fail(ErrUnsupported)
		}
		if e = s.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec(migration); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO metadata VALUES('migration_001_sha256',?)", hash([]byte(migration)))
			return e
		}); e != nil {
			return fail(e)
		}
	} else if version < 1 || version > 12 {
		return fail(ErrUnsupported)
	}
	var checksum string
	if e = s.db.QueryRow("SELECT value FROM metadata WHERE key='migration_001_sha256'").Scan(&checksum); e != nil || checksum != hash([]byte(migration)) {
		return fail(ErrUnsupported)
	}
	if version == 0 || version == 1 {
		if e = s.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec(migrationTwo); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO metadata VALUES('migration_002_sha256',?)", hash([]byte(migrationTwo)))
			return e
		}); e != nil {
			return fail(e)
		}
	}
	if e = s.db.QueryRow("SELECT value FROM metadata WHERE key='migration_002_sha256'").Scan(&checksum); e != nil || checksum != hash([]byte(migrationTwo)) {
		return fail(ErrUnsupported)
	}
	if version < 3 {
		if e = s.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec(migrationThree); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO metadata VALUES('migration_003_sha256',?)", hash([]byte(migrationThree)))
			return e
		}); e != nil {
			return fail(e)
		}
	}
	if e = s.db.QueryRow("SELECT value FROM metadata WHERE key='migration_003_sha256'").Scan(&checksum); e != nil || checksum != hash([]byte(migrationThree)) {
		return fail(ErrUnsupported)
	}
	if version < 4 {
		if e = s.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec(migrationFour); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO metadata VALUES('migration_004_sha256',?)", hash([]byte(migrationFour)))
			return e
		}); e != nil {
			return fail(e)
		}
	}
	if e = s.db.QueryRow("SELECT value FROM metadata WHERE key='migration_004_sha256'").Scan(&checksum); e != nil || checksum != hash([]byte(migrationFour)) {
		return fail(ErrUnsupported)
	}
	if version < 5 {
		if e = s.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec(migrationFive); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO metadata VALUES('migration_005_sha256',?)", hash([]byte(migrationFive)))
			return e
		}); e != nil {
			return fail(e)
		}
	}
	if e = s.db.QueryRow("SELECT value FROM metadata WHERE key='migration_005_sha256'").Scan(&checksum); e != nil || checksum != hash([]byte(migrationFive)) {
		return fail(ErrUnsupported)
	}
	if version < 6 {
		if e = s.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec(migrationSix); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO metadata VALUES('migration_006_sha256',?)", hash([]byte(migrationSix)))
			return e
		}); e != nil {
			return fail(e)
		}
	}
	if e = s.db.QueryRow("SELECT value FROM metadata WHERE key='migration_006_sha256'").Scan(&checksum); e != nil || checksum != hash([]byte(migrationSix)) {
		return fail(ErrUnsupported)
	}
	if version < 7 {
		if e = s.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec(migrationSeven); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO metadata VALUES('migration_007_sha256',?)", hash([]byte(migrationSeven)))
			return e
		}); e != nil {
			return fail(e)
		}
	}
	if e = s.db.QueryRow("SELECT value FROM metadata WHERE key='migration_007_sha256'").Scan(&checksum); e != nil || checksum != hash([]byte(migrationSeven)) {
		return fail(ErrUnsupported)
	}
	if version < 8 {
		if e = s.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec(migrationEight); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO metadata VALUES('migration_008_sha256',?)", hash([]byte(migrationEight)))
			return e
		}); e != nil {
			return fail(e)
		}
	}
	if e = s.db.QueryRow("SELECT value FROM metadata WHERE key='migration_008_sha256'").Scan(&checksum); e != nil || checksum != hash([]byte(migrationEight)) {
		return fail(ErrUnsupported)
	}
	if version < 9 {
		if e = s.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec(migrationNine); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO metadata VALUES('migration_009_sha256',?)", hash([]byte(migrationNine)))
			return e
		}); e != nil {
			return fail(e)
		}
	}
	if e = s.db.QueryRow("SELECT value FROM metadata WHERE key='migration_009_sha256'").Scan(&checksum); e != nil || checksum != hash([]byte(migrationNine)) {
		return fail(ErrUnsupported)
	}
	if version < 10 {
		if e = s.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec(migrationTen); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO metadata VALUES('migration_010_sha256',?)", hash([]byte(migrationTen)))
			return e
		}); e != nil {
			return fail(e)
		}
	}
	if e = s.db.QueryRow("SELECT value FROM metadata WHERE key='migration_010_sha256'").Scan(&checksum); e != nil || checksum != hash([]byte(migrationTen)) {
		return fail(ErrUnsupported)
	}
	if version < 11 {
		if e = s.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec(migrationEleven); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO metadata VALUES('migration_011_sha256',?)", hash([]byte(migrationEleven)))
			return e
		}); e != nil {
			return fail(e)
		}
	}
	if e = s.db.QueryRow("SELECT value FROM metadata WHERE key='migration_011_sha256'").Scan(&checksum); e != nil || checksum != hash([]byte(migrationEleven)) {
		return fail(ErrUnsupported)
	}
	if version < 12 {
		if e = s.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec(migrationTwelve); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO metadata VALUES('migration_012_sha256',?)", hash([]byte(migrationTwelve)))
			return e
		}); e != nil {
			return fail(e)
		}
	}
	if e = s.db.QueryRow("SELECT value FROM metadata WHERE key='migration_012_sha256'").Scan(&checksum); e != nil || checksum != hash([]byte(migrationTwelve)) {
		return fail(ErrUnsupported)
	}
	if e = s.recover(); e != nil {
		return fail(e)
	}
	return s, nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var e error
	if s.db != nil {
		e = s.db.Close()
		s.db = nil
	}
	if s.lock != nil {
		if x := s.lock.Close(); e == nil {
			e = x
		}
		s.lock = nil
	}
	return e
}
func (s *Store) transaction(fn func(*sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrClosed
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		if e == errExpiredFence || e == errStageLimitFence {
			fence := e
			if e = tx.Commit(); e != nil {
				return e
			}
			if fence == errStageLimitFence {
				return ErrStageLimit
			}
			return ErrFenced
		}
		return e
	}
	return tx.Commit()
}

type queryRow interface{ QueryRow(string, ...any) *sql.Row }

func taskIn(q queryRow, taskID string) (Task, error) {
	var t Task
	e := q.QueryRow("SELECT id,project_id,goal,state,plan_revision,generation FROM tasks WHERE id=?", taskID).Scan(&t.ID, &t.ProjectID, &t.Goal, &t.State, &t.PlanRevision, &t.Generation)
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrNotFound
	}
	return t, e
}
func planIn(q queryRow, taskID string, rev int64) (stageplan.Snapshot, error) {
	var raw, h string
	e := q.QueryRow("SELECT snapshot,hash FROM plan_revisions WHERE task_id=? AND revision=?", taskID, rev).Scan(&raw, &h)
	if errors.Is(e, sql.ErrNoRows) {
		return stageplan.Snapshot{}, ErrNotFound
	}
	if e != nil {
		return stageplan.Snapshot{}, e
	}
	var p stageplan.Snapshot
	if json.Unmarshal([]byte(raw), &p) != nil || p.Hash != h || stageplan.VerifySnapshot(p) != nil {
		return p, ErrInvalid
	}
	return p, nil
}
func (s *Store) Task(taskID string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return Task{}, ErrClosed
	}
	return taskIn(s.db, taskID)
}
func (s *Store) Plan(taskID string, rev int64) (stageplan.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return stageplan.Snapshot{}, ErrClosed
	}
	return planIn(s.db, taskID, rev)
}
func event(tx *sql.Tx, t string, kind, run string, gen int64) error {
	if _, e := tx.Exec("UPDATE tasks SET next_event_seq=next_event_seq+1 WHERE id=?", t); e != nil {
		return e
	}
	_, e := tx.Exec("INSERT INTO events SELECT id,next_event_seq,?,?,? FROM tasks WHERE id=?", kind, run, gen, t)
	return e
}
func (s *Store) Create(key string, in CreateRequest) (Task, error) {
	return s.create(key, in, nil, nil)
}
func (s *Store) CreateCurrent(key string, in CreateRequest, expected DefaultStamp) (Task, error) {
	return s.create(key, in, &expected, nil)
}
func creationPayload(key string, in CreateRequest) (CreateRequest, string, error) {
	if !opaque(key) || !opaque(in.ProjectID) || in.Goal == "" || len(in.Goal) > 65536 || in.Plan.Revision != 1 || stageplan.VerifySnapshot(in.Plan) != nil {
		return in, "", ErrInvalid
	}
	if in.Budget != nil {
		b := *in.Budget
		if !initialBudget(b) {
			return in, "", ErrInvalid
		}
		in.Budget = &b
	}
	if in.Preset != nil {
		ref := *in.Preset
		if !presetID(ref.ID) || ref.Revision < 1 || len(ref.Hash) != 64 {
			return in, "", ErrInvalid
		}
		in.Preset = &ref
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return in, "", e
	}
	return in, hash(raw), nil
}

// LookupCreation reads an exact committed request without evaluating current
// defaults or creating an intent. The project is part of its identity.
func (s *Store) LookupCreation(key string, in CreateRequest) (Task, error) {
	in, payloadHash, e := creationPayload(key, in)
	if e != nil {
		return Task{}, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return Task{}, ErrClosed
	}
	var oldHash, taskID string
	e = s.db.QueryRow("SELECT payload_hash,task_id FROM idempotency WHERE project_id=? AND key=?", in.ProjectID, key).Scan(&oldHash, &taskID)
	if errors.Is(e, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if e != nil {
		return Task{}, e
	}
	if oldHash != payloadHash {
		return Task{}, ErrConflict
	}
	return taskIn(s.db, taskID)
}
func (s *Store) create(key string, in CreateRequest, expected *DefaultStamp, submission *creationSubmission) (Task, error) {
	in, payloadHash, e := creationPayload(key, in)
	if e != nil {
		return Task{}, e
	}
	var result Task
	e = s.transaction(func(tx *sql.Tx) error {
		var oldHash, taskID string
		if submission != nil {
			if e := checkSubmissionJournal(tx, submission, key, payloadHash, in); e != nil {
				return e
			}
			var oldKey, oldProject, oldPlan string
			e := tx.QueryRow("SELECT idempotency_key,project_id,plan_hash,payload_hash,task_id FROM task_submissions WHERE preview_id=?", submission.previewID).Scan(&oldKey, &oldProject, &oldPlan, &oldHash, &taskID)
			if e == nil {
				if oldKey != key || oldProject != in.ProjectID || oldPlan != in.Plan.Hash || oldHash != payloadHash {
					return ErrConflict
				}
				result, e = taskIn(tx, taskID)
				return e
			}
			if !errors.Is(e, sql.ErrNoRows) {
				return e
			}
		}
		e := tx.QueryRow("SELECT payload_hash,task_id FROM idempotency WHERE project_id=? AND key=?", in.ProjectID, key).Scan(&oldHash, &taskID)
		if e == nil {
			if oldHash != payloadHash {
				return ErrConflict
			}
			if e = saveSubmission(tx, submission, key, payloadHash, in, taskID); e != nil {
				return e
			}
			result, e = taskIn(tx, taskID)
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if submission != nil && submission.existingOnly {
			return ErrNotFound
		}
		if e = checkDefaults(tx, in.ProjectID, expected); e != nil {
			return e
		}
		if in.Preset != nil {
			p, e := presetIn(tx, in.ProjectID, in.Preset.ID, in.Preset.Revision)
			if e != nil {
				return e
			}
			if p.Hash != in.Preset.Hash {
				return ErrConflict
			}
		}
		taskID, e = id("task-")
		if e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO tasks(id,project_id,goal,state,plan_revision) VALUES(?,?,?,'ready',1)", taskID, in.ProjectID, in.Goal); e != nil {
			return e
		}
		snapshot, _ := json.Marshal(in.Plan)
		if _, e = tx.Exec("INSERT INTO plan_revisions VALUES(?,?,?,?)", taskID, 1, string(snapshot), in.Plan.Hash); e != nil {
			return e
		}
		if in.Budget != nil {
			if _, e = tx.Exec("INSERT INTO task_budgets(task_id,max_calls,max_reworks) VALUES(?,?,?)", taskID, in.Budget.MaxCalls, in.Budget.MaxReworks); e != nil {
				return e
			}
		}
		if in.Preset != nil {
			if _, e = tx.Exec("INSERT INTO task_preset_refs VALUES(?,?,?,?,?)", taskID, in.ProjectID, in.Preset.ID, in.Preset.Revision, in.Preset.Hash); e != nil {
				return e
			}
		}
		if _, e = tx.Exec("INSERT INTO idempotency VALUES(?,?,?,?)", in.ProjectID, key, payloadHash, taskID); e != nil {
			return e
		}
		if e = event(tx, taskID, "created", "", 0); e != nil {
			return e
		}
		if e = saveSubmission(tx, submission, key, payloadHash, in, taskID); e != nil {
			return e
		}
		result, e = taskIn(tx, taskID)
		return e
	})
	return result, e
}
func (s *Store) RevisePlan(taskID string, ifMatch int64, p stageplan.Snapshot) error {
	return s.revisePlan(taskID, ifMatch, p, nil)
}
func (s *Store) RevisePlanCurrent(taskID string, ifMatch int64, p stageplan.Snapshot, expected DefaultStamp) error {
	return s.revisePlan(taskID, ifMatch, p, &expected)
}
func (s *Store) revisePlan(taskID string, ifMatch int64, p stageplan.Snapshot, expected *DefaultStamp) error {
	return s.transaction(func(tx *sql.Tx) error {
		t, e := revisionIn(tx, taskID, ifMatch, p)
		if e != nil {
			return e
		}
		if e = checkDefaults(tx, t.ProjectID, expected); e != nil {
			return e
		}
		raw, e := json.Marshal(p)
		if e != nil {
			return ErrInvalid
		}
		if _, e = tx.Exec("INSERT INTO plan_revisions VALUES(?,?,?,?)", taskID, p.Revision, string(raw), p.Hash); e != nil {
			return e
		}
		if _, e = tx.Exec("UPDATE tasks SET plan_revision=? WHERE id=?", p.Revision, taskID); e != nil {
			return e
		}
		return event(tx, taskID, "plan_revised", "", t.Generation)
	})
}

func (s *Store) Events(taskID string, after int64) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrClosed
	}
	if after < 0 {
		return nil, ErrInvalid
	}
	rows, e := s.db.Query("SELECT task_id,seq,kind,run_id,generation FROM events WHERE task_id=? AND seq>? ORDER BY seq", taskID, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.TaskID, &v.Seq, &v.Kind, &v.RunID, &v.Generation); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) SaveRoute(r stageplan.Route) error {
	if !opaque(r.ID) || r.Revision < 1 {
		return ErrInvalid
	}
	raw, e := json.Marshal(r)
	if e != nil {
		return e
	}
	return s.transaction(func(tx *sql.Tx) error {
		var old string
		e := tx.QueryRow("SELECT hash FROM route_revisions WHERE id=? AND revision=?", r.ID, r.Revision).Scan(&old)
		if e == nil {
			if old != hash(raw) {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		_, e = tx.Exec("INSERT INTO route_revisions VALUES(?,?,?,?)", r.ID, r.Revision, string(raw), hash(raw))
		return e
	})
}
func (s *Store) AddEvidence(v EvidenceRef) error {
	if !opaque(v.ID) || !opaque(v.TaskID) || !opaque(v.RunID) || !validHash(v.ArtifactHash) || !validHash(v.ReportHash) {
		return ErrInvalid
	}
	return s.transaction(func(tx *sql.Tx) error {
		r, e := runIn(tx, v.RunID)
		if e != nil {
			return e
		}
		if r.TaskID != v.TaskID {
			return ErrInvalid
		}
		var old EvidenceRef
		e = tx.QueryRow("SELECT id,task_id,run_id,artifact_hash,report_hash FROM evidence_refs WHERE id=?", v.ID).Scan(&old.ID, &old.TaskID, &old.RunID, &old.ArtifactHash, &old.ReportHash)
		if e == nil {
			if old != v {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		_, e = tx.Exec("INSERT INTO evidence_refs VALUES(?,?,?,?,?)", v.ID, v.TaskID, v.RunID, v.ArtifactHash, v.ReportHash)
		return e
	})
}
func validHash(v string) bool {
	b, e := hex.DecodeString(v)
	return e == nil && len(b) == 32 && v == strings.ToLower(v)
}
