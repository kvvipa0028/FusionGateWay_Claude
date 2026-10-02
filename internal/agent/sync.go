package agent

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"

	"github.com/yetone/magpie/internal/edit"
	"gopkg.in/yaml.v3"
)

// Most agents can't ask the gateway which models it has: magpie writes the
// catalog into a file of the agent's own (Pi's models.json, OpenCode's
// provider block, Codex's model catalog …) when a model is picked through
// magpie. That list was the catalog as it was then, so a provider added
// later never reached the agent, and one removed stayed listed. SyncCatalog
// rewrites each such list whenever the catalog may have changed.

var syncing struct {
	sync.Mutex
	running, again bool
}

// SyncCatalog brings every model list magpie wrote into an agent's files up
// to the catalog as it is now. It is what catalog.Changed is set to, so a
// provider saved or removed, or a vendor's list fetched anew, reaches the
// agents; one already running asks for another round rather than waiting.
// Errors are left for the picker: a file magpie can't write now is one the
// next change, or a pick, writes.
func SyncCatalog() {
	syncing.Lock()
	if syncing.running {
		syncing.again = true
		syncing.Unlock()
		return
	}
	syncing.running = true
	syncing.Unlock()
	for {
		for _, a := range All() {
			if a.Sync != nil {
				_ = a.Sync()
			}
		}
		syncing.Lock()
		if !syncing.again {
			syncing.running = false
			syncing.Unlock()
			return
		}
		syncing.again = false
		syncing.Unlock()
	}
}

// syncJSON rewrites the value at key of a JSON file, if the file has one
// there and it differs: an agent that watches its files isn't told of a
// change that isn't one.
func syncJSON(path, key string, value func() any) error {
	cur, ok := edit.GetJSON(path, key)
	if !ok {
		return nil
	}
	v := value()
	if sameJSON(cur, v) {
		return nil
	}
	return edit.SetJSON(path, edit.KV{Path: key, Value: v})
}

// sameJSON reports whether raw JSON says what v marshals to.
func sameJSON(raw string, v any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	var x, y any
	if json.Unmarshal([]byte(raw), &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// syncYAML is syncJSON for a YAML file.
func syncYAML(path, key string, value func() any) error {
	raw, err := edit.Read(path)
	if err != nil || raw == nil {
		return nil
	}
	var doc any
	if yaml.Unmarshal(raw, &doc) != nil {
		return nil
	}
	for _, k := range strings.Split(key, ".") {
		m, _ := doc.(map[string]any)
		if doc = m[k]; doc == nil {
			return nil
		}
	}
	v := value()
	b, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	var want any
	if yaml.Unmarshal(b, &want) == nil && reflect.DeepEqual(doc, want) {
		return nil
	}
	return edit.SetYAML(path, edit.KV{Path: key, Value: v})
}
