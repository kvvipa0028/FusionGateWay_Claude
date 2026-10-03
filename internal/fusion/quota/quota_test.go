package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureMeta() Meta {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	return Meta{Identity: Identity{Provider: "fixture-provider", Account: "fixture-account", Workspace: "fixture-workspace", Region: "fixture-region", Generation: 1}, Source: "fixture-native-read", ObservedAt: &now, ReceivedAt: now, Pool: Pool{ID: "fixture-pool", Provider: "fixture-provider", Region: "fixture-region", Scope: "account", Owner: "fixture-account", Verified: true}, Complete: true}
}
func TestEmptyFieldsAndCacheAgeNeverBecomeFreshQuota(t *testing.T) {
	m := fixtureMeta()
	for _, raw := range []string{`{}`, `{"windows":[]}`, `{"windows":[{"name":"5 hours"}]}`, `{"windows":[{"name":"5 hours","used":null}]}`} {
		s, e := AdaptMagpie([]byte(raw), m)
		if e != nil {
			t.Fatal(e)
		}
		if State(s, m.ReceivedAt, time.Minute) != Unknown {
			t.Fatalf("empty available: %+v", s)
		}
	}
	m = fixtureMeta()
	old := m.ReceivedAt.Add(-time.Hour)
	raw := `{"asOf":"` + old.Format(time.RFC3339) + `","kind":"subscription","windows":[{"name":"5 hours","used":25}]}`
	s, e := AdaptMagpie([]byte(raw), m)
	if e != nil {
		t.Fatal(e)
	}
	if State(s, m.ReceivedAt, time.Minute) != Stale || !s.ObservedAt.Equal(old) {
		t.Fatal("poll rejuvenated cache")
	}
	m = fixtureMeta()
	m.ObservedAt = nil
	s, _ = AdaptMagpie([]byte(`{"kind":"subscription","windows":[{"name":"5 hours","used":0}]}`), m)
	if State(s, m.ReceivedAt, time.Minute) != Unknown {
		t.Fatal("receipt used as observation")
	}
}
func TestQuotaStatesUnitsAndRawWindowsRemainDistinct(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want Status
	}{{`{"kind":"subscription","windows":[{"name":"5 hours","used":100}]}`, Zero}, {`{"kind":"subscription","windows":[{"name":"5 hours","used":0}]}`, Available}, {`{"kind":"balance","balance":"20 USD","windows":[]}`, Unknown}, {`{"kind":"subscription","windows":[{"name":"5 hours","used":101}]}`, Unknown}, {`{"kind":"subscription","windows":[{"name":"5 hours","used":-1}]}`, Unknown}} {
		m := fixtureMeta()
		s, e := AdaptMagpie([]byte(tc.raw), m)
		if e != nil {
			t.Fatal(e)
		}
		if got := State(s, m.ReceivedAt, time.Minute); got != tc.want {
			t.Fatalf("%s got %s", tc.raw, got)
		}
		if len(s.Windows) > 0 && len(s.Windows[0].Raw) == 0 {
			t.Fatal("raw window lost")
		}
	}
	m := fixtureMeta()
	m.Pool.Verified = false
	s, _ := AdaptMagpie([]byte(`{"kind":"subscription","windows":[{"name":"5 hours","used":0}]}`), m)
	if State(s, m.ReceivedAt, time.Minute) != Unverified {
		t.Fatal("guessed pool")
	}
	for _, st := range []Status{AuthRequired, Unsupported, Unknown} {
		s.Status = st
		if State(s, m.ReceivedAt, time.Minute) != st {
			t.Fatal("state conflated")
		}
	}
}
func TestCodexAndGLMAdaptersKeepMissingValuesAndSeparateMCP(t *testing.T) {
	m := fixtureMeta()
	s, e := AdaptCodex([]byte(`{"rateLimits":{"limitId":"fixture-limit","primary":{"usedPercent":20,"windowDurationMins":300,"resetsAt":1790989200}},"rateLimitsByLimitId":{"fixture-limit":{"primary":{"usedPercent":50,"windowDurationMins":300,"resetsAt":1790989200}}}}`), m)
	if e != nil || len(s.Windows) != 1 || *s.Windows[0].UsedPercent != 50 {
		t.Fatalf("legacy double count: %+v %v", s, e)
	}
	if State(s, m.ReceivedAt, time.Minute) != Available {
		t.Fatal("codex parse")
	}
	s, _ = AdaptCodex([]byte(`{"rateLimits":{"primary":{"windowDurationMins":300}}}`), m)
	if State(s, m.ReceivedAt, time.Minute) != Unknown {
		t.Fatal("codex missing defaulted")
	}
	s, e = AdaptGLM([]byte(`{"success":true,"data":{"level":"fixture-plan","limits":[{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":50,"nextResetTime":1790989200000},{"type":"TOKENS_LIMIT","unit":6,"number":1,"percentage":100},{"type":"TIME_LIMIT","percentage":20}]}}`), m)
	if e != nil || len(s.Windows) != 3 || s.Windows[2].Kind != "mcp" || State(s, m.ReceivedAt, time.Minute) != Zero {
		t.Fatalf("%+v %v", s, e)
	}
	s, _ = AdaptGLM([]byte(`{"success":true,"data":{"limits":[{"unit":3,"number":5}]}}`), m)
	if State(s, m.ReceivedAt, time.Minute) != Unknown {
		t.Fatal("glm missing defaulted")
	}
}
func TestSharedPoolAliasesAreNeverAddedAndUnknownPoolNotMerged(t *testing.T) {
	m := fixtureMeta()
	a, _ := AdaptMagpie([]byte(`{"kind":"subscription","windows":[{"name":"5 hours","used":25}]}`), m)
	b := clone(a)
	b.Identity.Account = "fixture-alias"
	b.ObservedAt = timePtr(m.ReceivedAt.Add(time.Second))
	b.ReceivedAt = *b.ObservedAt
	b.Windows[0].UsedPercent = floatPtr(40)
	pools := GroupPools([]Snapshot{a, b})
	if len(pools) != 1 || len(pools[0].Aliases) != 2 || *pools[0].Snapshot.Windows[0].UsedPercent != 40 {
		t.Fatal("shared pool summed or stale won")
	}
	a.Pool.Verified = false
	b.Pool.Verified = false
	if len(GroupPools([]Snapshot{a, b})) != 2 {
		t.Fatal("unverified windows guessed shared")
	}
	b.Pool.Verified = true
	b.Pool.Owner = "fixture-other-owner"
	a.Pool.Verified = true
	if len(GroupPools([]Snapshot{a, b})) != 2 {
		t.Fatal("account collision")
	}
}
func TestBrokerCoalescesAndDiscardsOldIdentityGeneration(t *testing.T) {
	m := fixtureMeta()
	b := NewBroker(time.Second)
	b.SetIdentity(m.Identity)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	fetch := func(context.Context, Identity) (Snapshot, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return AdaptMagpie([]byte(`{"kind":"subscription","windows":[{"name":"5 hours","used":25}]}`), m)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := b.Refresh(context.Background(), m.Identity, fetch); errs <- e }()
	}
	<-started
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("calls %d", calls.Load())
	}
	m.Identity.Generation = 2
	b.SetIdentity(m.Identity)
	if _, e := b.Current(m.Identity); !errors.Is(e, ErrNoSnapshot) {
		t.Fatal("old generation displayed")
	}
	old := m.Identity
	started = make(chan struct{})
	release = make(chan struct{})
	ch := make(chan error, 1)
	go func() {
		_, e := b.Refresh(context.Background(), old, func(context.Context, Identity) (Snapshot, error) {
			close(started)
			<-release
			return Snapshot{Identity: old}, nil
		})
		ch <- e
	}()
	<-started
	next := old
	next.Generation++
	b.SetIdentity(next)
	close(release)
	if e := <-ch; !errors.Is(e, ErrSuperseded) {
		t.Fatal(e)
	}
}
func TestBrokerDeadlineAndFailuresDoNotReplaceObservationTime(t *testing.T) {
	m := fixtureMeta()
	b := NewBroker(20 * time.Millisecond)
	b.SetIdentity(m.Identity)
	_, e := b.Refresh(context.Background(), m.Identity, func(ctx context.Context, _ Identity) (Snapshot, error) { <-ctx.Done(); return Snapshot{}, ctx.Err() })
	if e == nil {
		t.Fatal("unbounded refresh")
	}
	b = NewBroker(time.Second)
	b.SetIdentity(m.Identity)
	s, _ := AdaptMagpie([]byte(`{"kind":"subscription","windows":[{"name":"5 hours","used":25}]}`), m)
	_, e = b.Refresh(context.Background(), m.Identity, func(context.Context, Identity) (Snapshot, error) { return s, nil })
	if e != nil {
		t.Fatal(e)
	}
	_, e = b.Refresh(context.Background(), m.Identity, func(context.Context, Identity) (Snapshot, error) { return Snapshot{}, errors.New("fixture failed") })
	if e == nil {
		t.Fatal("failure hidden")
	}
	got, e := b.Current(m.Identity)
	if e != nil || !got.ObservedAt.Equal(*s.ObservedAt) || got.Status != Stale {
		t.Fatal("failed poll rejuvenated quota")
	}
}
func TestQuotaJSONRejectsAmbiguityAndFutureClock(t *testing.T) {
	m := fixtureMeta()
	for _, raw := range []string{`{"windows":[],"windows":[{"used":0}]}`, `{} {}`, `null`, "{"} {
		if _, e := AdaptMagpie([]byte(raw), m); e == nil {
			t.Fatal("ambiguous quota accepted")
		}
	}
	future := m.ReceivedAt.Add(time.Hour)
	m.ObservedAt = &future
	s, _ := AdaptMagpie([]byte(`{"kind":"subscription","windows":[{"name":"5 hours","used":0}]}`), m)
	if State(s, m.ReceivedAt, time.Minute) != Unknown {
		t.Fatal("future timestamp accepted")
	}
	raw, _ := json.Marshal(s)
	if !json.Valid(raw) {
		t.Fatal("schema JSON")
	}
}

func TestContradictoryRemainingAndMalformedSourceTimestampCannotAdmit(t *testing.T) {
	m := fixtureMeta()
	for _, raw := range []string{`{"kind":"subscription","asOf":"invalid","windows":[{"used":0}]}`, `{"kind":"subscription","windows":[{"used":0,"remaining":0}]}`, `{"kind":"subscription","windows":[{"used":0,"unlimited":true}]}`} {
		s, e := AdaptMagpie([]byte(raw), m)
		if e == nil && State(s, m.ReceivedAt, time.Minute) == Available {
			t.Errorf("unsafe reading admitted: %s", raw)
		}
	}
	s, e := AdaptGLM([]byte(`{"success":true,"data":{"limits":[{"type":"UNKNOWN_CREDITS","percentage":0}]}}`), m)
	if e == nil && State(s, m.ReceivedAt, time.Minute) == Available {
		t.Error("unknown GLM quota kind admitted")
	}
}
func TestNewPartialPoolObservationDoesNotHideBehindOlderGoodAlias(t *testing.T) {
	m := fixtureMeta()
	old, _ := AdaptMagpie([]byte(`{"kind":"subscription","windows":[{"used":0}]}`), m)
	fresh := clone(old)
	fresh.Complete = false
	fresh.ObservedAt = timePtr(m.ReceivedAt.Add(time.Second))
	fresh.ReceivedAt = *fresh.ObservedAt
	grouped := GroupPools([]Snapshot{old, fresh})
	if State(grouped[0].Snapshot, fresh.ReceivedAt, time.Minute) == Available {
		t.Error("partial new reading hidden by good alias")
	}
}
func TestBrokerDoesNotEchoReaderSecrets(t *testing.T) {
	m := fixtureMeta()
	b := NewBroker(time.Second)
	b.SetIdentity(m.Identity)
	_, e := b.Refresh(context.Background(), m.Identity, func(context.Context, Identity) (Snapshot, error) {
		return Snapshot{}, errors.New("fixture-secret-should-not-leak")
	})
	if e == nil || e.Error() == "fixture-secret-should-not-leak" {
		t.Error("raw reader error escaped")
	}
}

func TestRetiredAccountResponseAndGenerationRollbackAreRefused(t *testing.T) {
	m := fixtureMeta()
	b := NewBroker(time.Second)
	b.SetIdentity(m.Identity)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, e := b.Refresh(context.Background(), m.Identity, func(context.Context, Identity) (Snapshot, error) {
			close(started)
			<-release
			return AdaptMagpie([]byte(`{"kind":"subscription","windows":[{"used":0}]}`), m)
		})
		done <- e
	}()
	<-started
	b.Retire(m.Identity)
	next := m.Identity
	next.Account = "fixture-other"
	next.Generation++
	b.SetIdentity(next)
	close(release)
	if e := <-done; !errors.Is(e, ErrSuperseded) {
		t.Fatal(e)
	}
	if e := b.SetIdentity(m.Identity); e == nil {
		t.Fatal("retired generation reintroduced")
	}
}

func TestBalancesAndCreditsArePreservedWithoutBecomingModelQuota(t *testing.T) {
	m := fixtureMeta()
	s, e := AdaptMagpie([]byte(`{"kind":"balance","balance":"fixture-20 USD","windows":[]}`), m)
	if e != nil || len(s.Resources) != 1 || s.Resources[0].Kind != "balance" || s.Resources[0].Display != "fixture-20 USD" || State(s, m.ReceivedAt, time.Minute) != Unknown {
		t.Fatalf("%+v %v", s, e)
	}
	s, e = AdaptCodex([]byte(`{"rateLimits":{"credits":{"balance":"fixture-5","hasCredits":true}}}`), m)
	if e != nil || len(s.Resources) != 1 || s.Resources[0].Kind != "credits" || State(s, m.ReceivedAt, time.Minute) != Unknown {
		t.Fatalf("%+v %v", s, e)
	}
}

func TestPublishedQuotaStatusIsNotOptimisticAndZeroExpires(t *testing.T) {
	m := fixtureMeta()
	s, _ := AdaptMagpie([]byte(`{}`), m)
	if s.Status != Unknown {
		t.Error("empty published as available")
	}
	s, _ = AdaptMagpie([]byte(`{"kind":"subscription","windows":[{"used":100}]}`), m)
	if s.Status != Zero {
		t.Error("zero not explicit")
	}
	if State(s, m.ReceivedAt.Add(2*time.Minute), time.Minute) != Stale {
		t.Error("old zero not stale")
	}
	s, _ = AdaptMagpie([]byte(`{"kind":"subscription","windows":[{"used":101}]}`), m)
	if s.Windows[0].UsedPercent != nil || s.Status != Unknown {
		t.Error("invalid value escaped normalized fields")
	}
}

func TestWrappedQuotaErrorsCannotLeakSecrets(t *testing.T) {
	m := fixtureMeta()
	b := NewBroker(time.Second)
	b.SetIdentity(m.Identity)
	for _, cause := range []error{context.DeadlineExceeded, ErrMalformed, &QueryError{Status: AuthRequired}} {
		_, e := b.Refresh(context.Background(), m.Identity, func(context.Context, Identity) (Snapshot, error) {
			return Snapshot{}, fmt.Errorf("fixture-secret: %w", cause)
		})
		if e == nil || strings.Contains(e.Error(), "fixture-secret") {
			t.Error("wrapped secret escaped")
		}
	}
}
