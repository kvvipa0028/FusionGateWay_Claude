// Package quota adapts explicit read results; it never discovers authentication,
// reads browser cookies, refreshes credentials or invokes a legacy global poller.
package quota

import (
	"encoding/json"
	"math"
	"time"
)

type Status string

const (
	Available    Status = "available"
	Zero         Status = "zero"
	Unknown      Status = "unknown"
	Stale        Status = "stale"
	Unverified   Status = "unverified"
	AuthRequired Status = "auth_required"
	Unsupported  Status = "unsupported"
)

type Identity struct {
	Provider   string `json:"provider"`
	Account    string `json:"account"`
	Workspace  string `json:"workspace"`
	Region     string `json:"region"`
	Generation int64  `json:"generation"`
}
type Pool struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Region   string `json:"region"`
	Scope    string `json:"scope"`
	Owner    string `json:"owner"`
	Verified bool   `json:"verified"`
}
type Meta struct {
	Identity   Identity
	Source     string
	ObservedAt *time.Time
	ReceivedAt time.Time
	Pool       Pool
	Complete   bool
}
type Window struct {
	Name             string          `json:"name"`
	Kind             string          `json:"kind"`
	Unit             string          `json:"unit"`
	UsedPercent      *float64        `json:"used_percent"`
	RemainingPercent *float64        `json:"remaining_percent"`
	SpanSeconds      *int64          `json:"span_seconds"`
	ResetAt          *time.Time      `json:"reset_at"`
	Raw              json.RawMessage `json:"raw"`
}
type Resource struct {
	Kind    string          `json:"kind"`
	Unit    string          `json:"unit"`
	Display string          `json:"display"`
	Raw     json.RawMessage `json:"raw"`
}
type Snapshot struct {
	Identity   Identity   `json:"identity"`
	Source     string     `json:"source"`
	ObservedAt *time.Time `json:"observed_at"`
	ReceivedAt time.Time  `json:"received_at"`
	Pool       Pool       `json:"pool"`
	Complete   bool       `json:"complete"`
	Status     Status     `json:"status"`
	Windows    []Window   `json:"windows"`
	Resources  []Resource `json:"resources"`
}

func clone[T any](in T) T {
	raw, _ := json.Marshal(in)
	var out T
	json.Unmarshal(raw, &out)
	return out
}
func floatPtr(v float64) *float64    { return &v }
func timePtr(v time.Time) *time.Time { return &v }
func base(m Meta) Snapshot {
	return clone(Snapshot{Identity: m.Identity, Source: m.Source, ObservedAt: m.ObservedAt, ReceivedAt: m.ReceivedAt, Pool: m.Pool, Complete: m.Complete, Status: Available, Windows: []Window{}, Resources: []Resource{}})
}
func validPercent(v *float64) bool {
	return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0 && *v <= 100
}
func verifiedPool(p Pool, i Identity) bool {
	return p.Verified && p.ID != "" && p.Provider == i.Provider && p.Region == i.Region && p.Owner != "" && (p.Scope == "account" || p.Scope == "workspace" || p.Scope == "team")
}

// State never converts a reset time into replenished quota or tokens/money into
// subscription capacity. It is evaluated against source age at admission time.
func State(s Snapshot, now time.Time, maxAge time.Duration) Status {
	if s.Status != Available && s.Status != Zero {
		return s.Status
	}
	if s.Source == "" || s.ObservedAt == nil || s.ObservedAt.IsZero() || s.ReceivedAt.IsZero() || s.ObservedAt.After(s.ReceivedAt) || s.ObservedAt.After(now) || maxAge <= 0 {
		return Unknown
	}
	if now.Sub(*s.ObservedAt) > maxAge {
		return Stale
	}
	if !s.Complete || !verifiedPool(s.Pool, s.Identity) {
		return Unverified
	}
	found, empty := false, false
	for _, w := range s.Windows {
		if w.Kind != "subscription" && w.Kind != "coding_plan" {
			continue
		}
		found = true
		if w.Unit != "percent" || !validPercent(w.UsedPercent) {
			return Unknown
		}
		if w.ResetAt != nil && !now.Before(*w.ResetAt) {
			return Stale
		}
		if *w.UsedPercent == 100 {
			empty = true
		}
	}
	if !found {
		return Unknown
	}
	if empty {
		return Zero
	}
	return Available
}

// The published snapshot uses a conservative one-minute source-age ceiling.
// An admission gate may require a shorter age but cannot rejuvenate this read.
func normalized(s Snapshot) (Snapshot, error) {
	s.Status = State(s, s.ReceivedAt, time.Minute)
	return s, nil
}

type PoolView struct {
	Pool     Pool       `json:"pool"`
	Aliases  []Identity `json:"aliases"`
	Snapshot Snapshot   `json:"snapshot"`
}

// GroupPools selects the latest observation of an explicit physical
// pool; aliases do not add quota. Unverified identities remain separate rows.
func GroupPools(in []Snapshot) []PoolView {
	var out []PoolView
	positions := map[string]int{}
	for _, s := range in {
		key := ""
		if verifiedPool(s.Pool, s.Identity) {
			p := s.Pool
			p.Verified = false
			raw, _ := json.Marshal(p)
			key = string(raw)
		}
		if pos, ok := positions[key]; ok && key != "" {
			v := &out[pos]
			v.Aliases = append(v.Aliases, clone(s.Identity))
			if s.ObservedAt != nil && (v.Snapshot.ObservedAt == nil || s.ObservedAt.After(*v.Snapshot.ObservedAt)) || s.ObservedAt == nil && s.ReceivedAt.After(v.Snapshot.ReceivedAt) {
				v.Snapshot = clone(s)
			}
			continue
		}
		if key != "" {
			positions[key] = len(out)
		}
		out = append(out, PoolView{Pool: clone(s.Pool), Aliases: []Identity{clone(s.Identity)}, Snapshot: clone(s)})
	}
	return out
}
