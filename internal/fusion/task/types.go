package task

import (
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"time"
)

type Task struct {
	ID           string `json:"id"`
	ProjectID    string `json:"project_id"`
	Goal         string `json:"goal"`
	State        string `json:"state"`
	PlanRevision int64  `json:"plan_revision"`
	Generation   int64  `json:"generation"`
}
type StageRun struct {
	ID              string                    `json:"id"`
	TaskID          string                    `json:"task_id"`
	Role            stageplan.Role            `json:"role"`
	Attempt         int64                     `json:"attempt"`
	Generation      int64                     `json:"generation"`
	PlanRevision    int64                     `json:"plan_revision"`
	State           string                    `json:"state"`
	NativeSessionID string                    `json:"native_session_id"`
	Owner           string                    `json:"owner"`
	LeaseUntil      time.Time                 `json:"lease_until"`
	StartupIntent   bool                      `json:"startup_intent"`
	LaunchConfirmed bool                      `json:"launch_confirmed"`
	Target          stageplan.ExecutionTarget `json:"target"`
}
type Event struct {
	TaskID     string `json:"task_id"`
	Seq        int64  `json:"seq"`
	Kind       string `json:"kind"`
	RunID      string `json:"run_id"`
	Generation int64  `json:"generation"`
}
type EvidenceRef struct {
	ID           string `json:"id"`
	TaskID       string `json:"task_id"`
	RunID        string `json:"run_id"`
	ArtifactHash string `json:"artifact_hash"`
	ReportHash   string `json:"report_hash"`
}
