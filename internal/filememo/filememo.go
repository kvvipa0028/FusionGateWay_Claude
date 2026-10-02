// Package filememo keeps what was parsed from a file until the file
// changes, so a page that asks many times over reads it once.
package filememo

import (
	"bytes"
	"os"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/steady"
)

type entry struct {
	mod  time.Time
	size int64
	v    any
	// the bytes parsed, while the file's time is too recent to tell a
	// second write by: Linux stamps a file from a clock a tick (a few
	// milliseconds) coarse, so two writes of one size inside a tick (two
	// accounts swapped) leave the same time and size
	raw []byte
}

// settled is how old a file's time must be for any later write to stamp
// a later one, many ticks of the coarsest clock.
const settled = 2 * time.Second

var (
	mu   sync.Mutex
	seen = map[string]entry{}
)

// Read is parse of the file at path, from what it gave last time when the
// file has the same size and time as then (and, written just now, the same
// bytes). kind tells apart two parses of
// one file. What it returns is shared: the caller must not change it.
func Read[T any](kind, path string, parse func([]byte) (T, error)) (T, error) {
	var zero T
	fi, err := os.Stat(path)
	if err != nil {
		return zero, err
	}
	key := kind + "\x00" + path
	mu.Lock()
	e, ok := seen[key]
	mu.Unlock()
	if ok && e.mod.Equal(fi.ModTime()) && e.size == fi.Size() && e.raw == nil {
		return e.v.(T), nil
	}
	b, err := steady.ReadFile(path)
	if err != nil {
		return zero, err
	}
	if ok && e.mod.Equal(fi.ModTime()) && bytes.Equal(b, e.raw) {
		if time.Since(e.mod) > settled {
			mu.Lock()
			e.raw = nil
			seen[key] = e
			mu.Unlock()
		}
		return e.v.(T), nil
	}
	v, err := parse(b)
	if err != nil {
		return zero, err
	}
	mu.Lock()
	e = entry{mod: fi.ModTime(), size: fi.Size(), v: v}
	if time.Since(e.mod) <= settled {
		e.raw = b
	}
	seen[key] = e
	mu.Unlock()
	return v, nil
}
