package quota

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

var ErrMalformed = errors.New("invalid quota response")

func unique(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrMalformed
	}
	v, e := d.Token()
	if e != nil {
		return ErrMalformed
	}
	delim, ok := v.(json.Delim)
	if !ok {
		return nil
	}
	if delim == '{' {
		seen := map[string]bool{}
		for d.More() {
			v, e = d.Token()
			k, ok := v.(string)
			k = strings.ToLower(k)
			if e != nil || !ok || seen[k] {
				return ErrMalformed
			}
			seen[k] = true
			if unique(d, depth+1) != nil {
				return ErrMalformed
			}
		}
		v, e = d.Token()
		if e != nil || v != json.Delim('}') {
			return ErrMalformed
		}
	} else if delim == '[' {
		for d.More() {
			if unique(d, depth+1) != nil {
				return ErrMalformed
			}
		}
		v, e = d.Token()
		if e != nil || v != json.Delim(']') {
			return ErrMalformed
		}
	} else {
		return ErrMalformed
	}
	return nil
}
func object(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > 1024*1024 {
		return nil, ErrMalformed
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if unique(d, 0) != nil {
		return nil, ErrMalformed
	}
	if _, e := d.Token(); e != io.EOF {
		return nil, ErrMalformed
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil, ErrMalformed
	}
	return m, nil
}
func text(raw json.RawMessage) string { var v string; json.Unmarshal(raw, &v); return v }
func number(raw json.RawMessage) *float64 {
	var v *float64
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}
func timestamp(raw json.RawMessage) *time.Time {
	var v *time.Time
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}
func unixTime(raw json.RawMessage, millis bool) *time.Time {
	n := number(raw)
	if n == nil || *n <= 0 {
		return nil
	}
	var v time.Time
	if millis {
		v = time.UnixMilli(int64(*n))
	} else {
		v = time.Unix(int64(*n), 0)
	}
	return &v
}
func rawWindow(m map[string]json.RawMessage, keys ...string) json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	raw, _ := json.Marshal(out)
	return raw
}
func setUsed(w *Window, n *float64) {
	if validPercent(n) {
		w.UsedPercent = n
		w.RemainingPercent = floatPtr(100 - *n)
	}
}

// AdaptMagpie consumes an explicitly supplied Quota JSON result. It never calls
// SubscriptionUsage: that legacy cache discovers/authenticates other accounts.
// Source Meta is supplied by an admitted reader; AsOf overrides newer polling.
func AdaptMagpie(raw []byte, m Meta) (Snapshot, error) {
	s := base(m)
	v, e := object(raw)
	if e != nil {
		return Snapshot{}, e
	}
	if asof := timestamp(v["asOf"]); asof != nil {
		s.ObservedAt = asof
	} else if raw, exists := v["asOf"]; exists {
		if string(raw) != "null" {
			return Snapshot{}, ErrMalformed
		}
		s.ObservedAt = nil
	}
	if text(v["error"]) != "" {
		s.Status = Unknown
	}
	kind := text(v["kind"])
	if balance := text(v["balance"]); balance != "" {
		s.Resources = append(s.Resources, Resource{Kind: "balance", Unit: "vendor_display", Display: balance, Raw: rawWindow(v, "balance")})
	}
	if kind == "plan" {
		kind = "coding_plan"
	}
	var windows []json.RawMessage
	if len(v["windows"]) > 0 && json.Unmarshal(v["windows"], &windows) != nil {
		return Snapshot{}, ErrMalformed
	}
	for _, raw := range windows {
		q, e := object(raw)
		if e != nil {
			return Snapshot{}, e
		}
		w := Window{Name: text(q["name"]), Kind: kind, Unit: "percent", ResetAt: timestamp(q["resetsAt"]), Raw: rawWindow(q, "name", "used", "remaining", "resetsAt", "display", "unlimited")}
		setUsed(&w, number(q["used"]))
		if rem, exists := q["remaining"]; exists {
			n := number(rem)
			if !validPercent(n) || !validPercent(w.UsedPercent) || math.Abs(*n+*w.UsedPercent-100) > 0.000001 {
				s.Status = Unknown
			} else {
				w.RemainingPercent = n
			}
		}
		if string(q["unlimited"]) == "true" {
			s.Status = Unknown
		}
		s.Windows = append(s.Windows, w)
	}
	return normalized(s)
}

// AdaptCodex prefers rateLimitsByLimitId when present; the legacy bucket is an
// alias, never additional quota. Native RPC observation metadata is explicit.
func AdaptCodex(raw []byte, m Meta) (Snapshot, error) {
	s := base(m)
	v, e := object(raw)
	if e != nil {
		return Snapshot{}, e
	}
	buckets := map[string]json.RawMessage{}
	if q, ok := v["rateLimitsByLimitId"]; ok && string(q) != "null" {
		if json.Unmarshal(q, &buckets) != nil {
			return Snapshot{}, ErrMalformed
		}
	} else if q, ok := v["rateLimits"]; ok && string(q) != "null" {
		buckets["legacy"] = q
	}
	// Stable sorting gives reproducible evidence without depending on map order.
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, id := range keys {
		q, e := object(buckets[id])
		if e != nil {
			return Snapshot{}, e
		}
		if raw, ok := q["credits"]; ok && string(raw) != "null" {
			credits, e := object(raw)
			if e != nil {
				return Snapshot{}, e
			}
			s.Resources = append(s.Resources, Resource{Kind: "credits", Unit: "vendor_display", Display: text(credits["balance"]), Raw: rawWindow(credits, "balance", "hasCredits", "unlimited")})
		}
		for _, name := range []string{"primary", "secondary"} {
			raw := q[name]
			if len(raw) == 0 || string(raw) == "null" {
				continue
			}
			window, e := object(raw)
			if e != nil {
				return Snapshot{}, e
			}
			w := Window{Name: id + "/" + name, Kind: "subscription", Unit: "percent", ResetAt: unixTime(window["resetsAt"], false), Raw: rawWindow(window, "usedPercent", "windowDurationMins", "resetsAt")}
			if n := number(window["windowDurationMins"]); n != nil && *n > 0 && *n <= 525600 {
				seconds := int64(*n * 60)
				w.SpanSeconds = &seconds
			}
			setUsed(&w, number(window["usedPercent"]))
			s.Windows = append(s.Windows, w)
		}
	}
	return normalized(s)
}

// AdaptGLM overlays field-presence semantics on the existing native plan quota
// shapes. Missing percentage stays unknown; an MCP window is not a model pool.
func AdaptGLM(raw []byte, m Meta) (Snapshot, error) {
	s := base(m)
	v, e := object(raw)
	if e != nil {
		return Snapshot{}, e
	}
	if q, ok := v["success"]; ok && string(q) != "true" {
		s.Status = Unknown
		return normalized(s)
	}
	data, e := object(v["data"])
	if e != nil {
		s.Status = Unknown
		return normalized(s)
	}
	var limits []json.RawMessage
	if q := data["limits"]; len(q) > 0 && json.Unmarshal(q, &limits) != nil {
		return Snapshot{}, ErrMalformed
	}
	for _, raw := range limits {
		q, e := object(raw)
		if e != nil {
			return Snapshot{}, e
		}
		w := Window{Name: "Allowance", Kind: "coding_plan", Unit: "percent", ResetAt: unixTime(q["nextResetTime"], true), Raw: rawWindow(q, "type", "unit", "number", "percentage", "nextResetTime")}
		if !strings.EqualFold(text(q["type"]), "TOKENS_LIMIT") {
			w.Kind = "unknown"
		}
		unit := number(q["unit"])
		n := number(q["number"])
		if strings.EqualFold(text(q["type"]), "TIME_LIMIT") {
			w.Name = "MCP"
			w.Kind = "mcp"
		} else if unit != nil && *unit == 3 && n != nil && *n > 0 && *n <= 8760 {
			span := int64(*n * 3600)
			w.SpanSeconds = &span
			w.Name = "hours"
		} else if unit != nil && *unit == 6 {
			span := int64(7 * 24 * 3600)
			w.SpanSeconds = &span
			w.Name = "week"
		}
		setUsed(&w, number(q["percentage"]))
		s.Windows = append(s.Windows, w)
	}
	return normalized(s)
}
