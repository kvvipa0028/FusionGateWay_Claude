package quota

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"
)

var (
	ErrNoSnapshot  = errors.New("quota snapshot unavailable")
	ErrSuperseded  = errors.New("quota identity generation superseded")
	ErrBusy        = errors.New("quota refresh limit reached")
	ErrQueryFailed = errors.New("quota query failed")
)

type QueryError struct{ Status Status }

func (*QueryError) Error() string { return "quota query failed" }

type Fetch func(context.Context, Identity) (Snapshot, error)
type flight struct {
	done     chan struct{}
	ctx      context.Context
	snapshot Snapshot
	err      error
}
type Broker struct {
	mu         sync.Mutex
	timeout    time.Duration
	generation map[string]int64
	cache      map[string]Snapshot
	pending    map[string]*flight
}

func NewBroker(timeout time.Duration) *Broker {
	if timeout <= 0 || timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	return &Broker{timeout: timeout, generation: map[string]int64{}, cache: map[string]Snapshot{}, pending: map[string]*flight{}}
}
func identityKey(i Identity, withGeneration bool) string {
	if !withGeneration {
		i.Generation = 0
	}
	raw, _ := json.Marshal(i)
	return string(raw)
}
func validIdentity(i Identity) bool {
	if i.Generation < 1 {
		return false
	}
	for _, s := range []string{i.Provider, i.Account, i.Workspace, i.Region} {
		if s == "" || len(s) > 256 || strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
			return false
		}
	}
	return true
}

func (b *Broker) SetIdentity(i Identity) error {
	if !validIdentity(i) {
		return ErrSuperseded
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	key := identityKey(i, false)
	old := b.generation[key]
	if i.Generation < old {
		return ErrSuperseded
	}
	if i.Generation > old {
		b.generation[key] = i.Generation
		for k, s := range b.cache {
			if identityKey(s.Identity, false) == key {
				delete(b.cache, k)
			}
		}
	}
	return nil
}

// Retire fences an old identity before switching account/workspace/region.
// A tombstone prevents a late callback from registering the old generation.
func (b *Broker) Retire(i Identity) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := identityKey(i, false)
	if b.generation[key] <= i.Generation {
		b.generation[key] = i.Generation + 1
	}
	delete(b.cache, identityKey(i, true))
}
func (b *Broker) Current(i Identity) (Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.generation[identityKey(i, false)] != i.Generation {
		return Snapshot{}, ErrSuperseded
	}
	s, ok := b.cache[identityKey(i, true)]
	if !ok {
		return Snapshot{}, ErrNoSnapshot
	}
	return clone(s), nil
}

// Refresh coalesces exact account/region/workspace/generation requests. The
// query timeout is independent of the first caller; one caller's cancellation
// cannot cancel the query awaited by other callers. A hung reader occupies its
// slot until it exits, so repeated requests cannot create unlimited goroutines.
func (b *Broker) Refresh(ctx context.Context, i Identity, fetch Fetch) (Snapshot, error) {
	if fetch == nil || !validIdentity(i) {
		return Snapshot{}, ErrSuperseded
	}
	b.mu.Lock()
	if b.generation[identityKey(i, false)] != i.Generation {
		b.mu.Unlock()
		return Snapshot{}, ErrSuperseded
	}
	key := identityKey(i, true)
	f := b.pending[key]
	if f == nil {
		if len(b.pending) >= 8 {
			b.mu.Unlock()
			return Snapshot{}, ErrBusy
		}
		work, cancel := context.WithTimeout(context.Background(), b.timeout)
		f = &flight{done: make(chan struct{}), ctx: work}
		b.pending[key] = f
		go func() {
			defer cancel()
			s, e := fetch(work, i)
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.pending, key)
			if b.generation[identityKey(i, false)] != i.Generation {
				e = ErrSuperseded
			} else if work.Err() != nil {
				e = work.Err()
			} else if e == nil && (s.Identity != i || s.Source == "" || s.ReceivedAt.IsZero()) {
				e = ErrMalformed
			}
			if e == nil {
				b.cache[key] = clone(s)
			} else if !errors.Is(e, ErrSuperseded) {
				if old, ok := b.cache[key]; ok {
					old.Status = Stale
					var q *QueryError
					if errors.As(e, &q) && (q.Status == AuthRequired || q.Status == Unsupported || q.Status == Unknown) {
						old.Status = q.Status
					}
					b.cache[key] = old
				}
			}
			e = safeQueryError(e)
			f.snapshot = clone(s)
			f.err = e
			close(f.done)
		}()
	}
	b.mu.Unlock()
	select {
	case <-f.done:
		return clone(f.snapshot), f.err
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	case <-f.ctx.Done():
		return Snapshot{}, f.ctx.Err()
	}
}

func safeQueryError(e error) error {
	if e == nil {
		return nil
	}
	for _, known := range []error{ErrSuperseded, ErrMalformed, ErrBusy, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(e, known) {
			return known
		}
	}
	var q *QueryError
	if errors.As(e, &q) && q != nil && (q.Status == AuthRequired || q.Status == Unsupported || q.Status == Unknown) {
		return &QueryError{Status: q.Status}
	}
	return ErrQueryFailed
}
