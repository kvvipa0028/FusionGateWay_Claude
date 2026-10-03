// Package stageplan compiles role configuration into copied, hash-bound targets.
// It does not admit native routes or authorize an execution by itself.
package stageplan

type Role string

const (
	Design         Role = "design"
	Implementation Role = "implementation"
	Testing        Role = "testing"
	Review         Role = "review"
	Acceptance     Role = "acceptance"
)

func AllRoles() []Role { return []Role{Design, Implementation, Testing, Review, Acceptance} }

type Group string

const (
	DesignPlanning        Group = "design_planning"
	ImplementationTesting Group = "implementation_testing"
	ReviewAcceptance      Group = "review_acceptance"
)

type Mode string

const (
	Locked  Mode = "locked"
	Auto    Mode = "auto"
	Inherit Mode = "inherit"
)

type EffortSelectionMode string

const (
	EffortExplicit EffortSelectionMode = "explicit"
	EffortDefault  EffortSelectionMode = "default"
	EffortNone     EffortSelectionMode = "none"
)

type LockEnforcement string

const (
	ControlledCalls LockEnforcement = "controlled_calls"
	PrimaryOnly     LockEnforcement = "primary_only"
	Unverified      LockEnforcement = "unverified"
)

type RouteRef struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}
type EffortSelection struct {
	Mode  EffortSelectionMode `json:"mode"`
	Value string              `json:"value,omitempty"`
}
type Candidate struct {
	Route  RouteRef         `json:"route"`
	Model  string           `json:"model"`
	Effort *EffortSelection `json:"effort"`
}
type Binding struct {
	Mode                 Mode             `json:"mode"`
	Route                *RouteRef        `json:"route,omitempty"`
	Model                string           `json:"model,omitempty"`
	Effort               *EffortSelection `json:"effort,omitempty"`
	Candidates           []Candidate      `json:"candidates,omitempty"`
	RequiredCapabilities []string         `json:"required_capabilities,omitempty"`
	AcceptPrimaryOnly    bool             `json:"accept_primary_only,omitempty"`
}
type Layer struct {
	Groups map[Group]Binding `json:"groups,omitempty"`
	Roles  map[Role]Binding  `json:"roles,omitempty"`
}
type Input struct {
	SchemaVersion int    `json:"schema_version"`
	Revision      int64  `json:"revision"`
	RequiredRoles []Role `json:"required_roles"`
	Global        Layer  `json:"global"`
	Project       Layer  `json:"project,omitempty"`
	Task          Layer  `json:"task,omitempty"`
}

// Route entries come only from a trusted, revisioned registry. Admitted is never
// accepted from user plan JSON; WP-08 owns admission evidence and lifecycle.
type Route struct {
	ID                 string          `json:"id"`
	Revision           int64           `json:"revision"`
	Model              string          `json:"model"`
	Account            string          `json:"account"`
	Workspace          string          `json:"workspace"`
	CredentialIdentity string          `json:"credential_identity"`
	RuntimeVersion     string          `json:"runtime_version"`
	PluginVersion      *string         `json:"plugin_version"`
	BillingPath        string          `json:"billing_path"`
	BillingKnown       bool            `json:"billing_known"`
	Admitted           bool            `json:"admitted"`
	Efforts            []string        `json:"efforts"`
	DefaultEffort      *string         `json:"default_effort"`
	NoEffort           bool            `json:"no_effort"`
	Capabilities       []string        `json:"capabilities"`
	LockEnforcement    LockEnforcement `json:"lock_enforcement"`
}
type FrozenEffort struct {
	RequestedMode EffortSelectionMode `json:"requested_mode"`
	Value         *string             `json:"value"`
}
type ExecutionTarget struct {
	Route                 RouteRef        `json:"route"`
	RequestedModel        string          `json:"requested_model"`
	ResolvedModel         string          `json:"resolved_model"`
	UpstreamReportedModel *string         `json:"upstream_reported_model"`
	Account               string          `json:"account"`
	Workspace             string          `json:"workspace"`
	CredentialIdentity    string          `json:"credential_identity"`
	Effort                FrozenEffort    `json:"effort"`
	BillingPath           string          `json:"billing_path"`
	RuntimeVersion        string          `json:"runtime_version"`
	PluginVersion         *string         `json:"plugin_version"`
	Capabilities          []string        `json:"capabilities"`
	LockEnforcement       LockEnforcement `json:"lock_enforcement"`
}
type FrozenBinding struct {
	Mode                 Mode              `json:"mode"`
	Source               string            `json:"source"`
	Target               *ExecutionTarget  `json:"target,omitempty"`
	Candidates           []ExecutionTarget `json:"candidates,omitempty"`
	RequiredCapabilities []string          `json:"required_capabilities,omitempty"`
	AcceptPrimaryOnly    bool              `json:"accept_primary_only,omitempty"`
}
type Snapshot struct {
	SchemaVersion int                    `json:"schema_version"`
	Revision      int64                  `json:"revision"`
	RequiredRoles []Role                 `json:"required_roles"`
	Bindings      map[Role]FrozenBinding `json:"bindings"`
	Hash          string                 `json:"hash,omitempty"`
}
