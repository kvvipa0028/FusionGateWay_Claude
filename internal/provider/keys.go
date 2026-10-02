package provider

// A provider can hold several accounts, as a subscription can: several API
// keys — a personal one and the team's, a paid plan and a free one. Any
// number of them can be on at once: requests go to the first, and when it
// is out of quota or rate limited the gateway moves on to the next one
// that's on (see gateway/fallback.go). Key stays the first, so everything
// else that talks to the vendor reads it as before.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// KeyAccount is a saved key after the first. Off keeps it without using it.
type KeyAccount struct {
	Name string `json:"name,omitempty"`
	Key  string `json:"key"`
	Off  bool   `json:"off,omitempty"`
	// Protocol, when set, is the only one the key works with: some relays
	// give out one key for Anthropic and another for OpenAI. Empty is any.
	Protocol Protocol `json:"protocol,omitempty"`
}

// KeyInfo describes one of a provider's keys without giving it away.
type KeyInfo struct {
	ID     string `json:"id"` // a fingerprint, to name it in a switch
	Name   string `json:"name,omitempty"`
	Masked string `json:"masked"`
	Active bool   `json:"active"` // the first, where requests go
	On     bool   `json:"on"`     // in use: the first, or next in line

	Protocol Protocol `json:"protocol,omitempty"` // the only one it works with
}

func keyID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:5])
}

// KeyID is the fingerprint a key is named by, in lists and in the gateway.
func KeyID(key string) string { return keyID(key) }

// KeyList is a provider's keys, in the order requests try them.
func (p Provider) KeyList() []KeyInfo {
	var out []KeyInfo
	if p.Key != "" {
		out = append(out, KeyInfo{ID: keyID(p.Key), Name: p.KeyName, Masked: Mask(p.Key), Active: true, On: true, Protocol: p.KeyProtocol})
	}
	for _, k := range p.Keys {
		out = append(out, KeyInfo{ID: keyID(k.Key), Name: k.Name, Masked: Mask(k.Key), On: !k.Off, Protocol: k.Protocol})
	}
	return out
}

// KeysOn is the keys in use, first to last: the first key, then every
// saved one that's on.
func (p Provider) KeysOn() []KeyAccount {
	if p.Key == "" {
		return nil
	}
	out := []KeyAccount{p.first()}
	for _, k := range p.Keys {
		if !k.Off && k.Key != "" {
			out = append(out, k)
		}
	}
	return out
}

// AddKey saves one more key for a provider, on: it takes requests after
// the ones before it. It becomes the first when the provider has none.
func AddKey(id, name, key string, proto Protocol) error {
	name, key = strings.TrimSpace(name), strings.TrimSpace(key)
	if err := keyProtocolOK(proto); err != nil {
		return err
	}
	if key == "" {
		return errors.New("paste the key to add")
	}
	p, err := Find(id)
	if err != nil {
		return err
	}
	if p.Account != nil {
		return errors.New("a signed-in account has no keys")
	}
	for _, k := range p.KeyList() {
		if k.ID == keyID(key) {
			return fmt.Errorf("%s already has this key", p.Name)
		}
	}
	if p.Key == "" {
		p.Key, p.KeyName, p.KeyProtocol = key, name, proto
	} else {
		p.Keys = append(p.Keys, KeyAccount{Name: name, Key: key, Protocol: proto})
	}
	return Save(*p)
}

// first is the first key as a KeyAccount, and setFirst makes k the first.
func (p *Provider) first() KeyAccount {
	return KeyAccount{Name: p.KeyName, Key: p.Key, Protocol: p.KeyProtocol}
}

func (p *Provider) setFirst(k KeyAccount) {
	p.Key, p.KeyName, p.KeyProtocol = k.Key, k.Name, k.Protocol
}

func keyProtocolOK(proto Protocol) error {
	switch proto {
	case "", Chat, Responses, Anthropic:
		return nil
	}
	return fmt.Errorf("unknown protocol %q", proto)
}

// SetKeyProtocol says which one protocol a key works with; empty is any.
func SetKeyProtocol(id, keyRef string, proto Protocol) error {
	if err := keyProtocolOK(proto); err != nil {
		return err
	}
	p, err := Find(id)
	if err != nil {
		return err
	}
	i, ok := findKey(p, keyRef)
	if !ok {
		return fmt.Errorf("%s has no such key", p.Name)
	}
	if i < 0 {
		p.KeyProtocol = proto
	} else {
		p.Keys[i].Protocol = proto
	}
	return Save(*p)
}

// WithKey is p using key k: its endpoints narrowed to k's protocol when k
// has one. It has none left when p doesn't serve that protocol.
func (p Provider) WithKey(k KeyAccount) Provider {
	p.Key, p.KeyName, p.KeyProtocol = k.Key, k.Name, k.Protocol
	if k.Protocol != "" {
		for _, pr := range Protocols {
			if pr != k.Protocol {
				switch pr {
				case Chat:
					p.Chat = ""
				case Responses:
					p.Responses = ""
				case Anthropic:
					p.Anthropic = ""
				}
			}
		}
	}
	return p
}

// findKey is where a key is among p.Keys: -1 for the first key, and ok
// false when p has no such key.
func findKey(p *Provider, ref string) (int, bool) {
	if p.Key != "" && keyID(p.Key) == ref {
		return -1, true
	}
	for i, k := range p.Keys {
		if keyID(k.Key) == ref {
			return i, true
		}
	}
	return 0, false
}

// UseKey makes one of a provider's keys the first, and turns it on; the
// one it replaces stays on, next in line.
func UseKey(id, keyRef string) error {
	p, err := Find(id)
	if err != nil {
		return err
	}
	i, ok := findKey(p, keyRef)
	if !ok {
		return fmt.Errorf("%s has no such key", p.Name)
	}
	if i < 0 {
		return nil
	}
	k := p.Keys[i]
	rest := append([]KeyAccount{p.first()}, append(p.Keys[:i:i], p.Keys[i+1:]...)...)
	p.setFirst(k)
	p.Keys = rest
	return Save(*p)
}

// promote takes the first key out of use: the next key that's on takes
// its place. ok is false when none is.
func promote(p *Provider) (old KeyAccount, ok bool) {
	for i, k := range p.Keys {
		if !k.Off {
			old = p.first()
			p.setFirst(k)
			p.Keys = append(p.Keys[:i:i], p.Keys[i+1:]...)
			return old, true
		}
	}
	return KeyAccount{}, false
}

// SetKeyOn turns a key on or off. The last key that's on stays on.
func SetKeyOn(id, keyRef string, on bool) error {
	p, err := Find(id)
	if err != nil {
		return err
	}
	i, ok := findKey(p, keyRef)
	if !ok {
		return fmt.Errorf("%s has no such key", p.Name)
	}
	switch {
	case i >= 0:
		p.Keys[i].Off = !on
	case !on:
		old, ok := promote(p)
		if !ok {
			return errors.New("that's the only key in use; turn another on first")
		}
		old.Off = true
		p.Keys = append([]KeyAccount{old}, p.Keys...)
	default:
		return nil
	}
	return Save(*p)
}

// RemoveKey forgets a key. The last key in use stays: turn another on first.
func RemoveKey(id, keyRef string) error {
	p, err := Find(id)
	if err != nil {
		return err
	}
	i, ok := findKey(p, keyRef)
	if !ok {
		return fmt.Errorf("%s has no such key", p.Name)
	}
	if i >= 0 {
		p.Keys = append(p.Keys[:i], p.Keys[i+1:]...)
	} else if _, ok := promote(p); !ok {
		return errors.New("that's the only key in use; turn another on before removing it")
	}
	return Save(*p)
}

// RenameKey names one of a provider's keys, "Personal", "Team".
func RenameKey(id, keyRef, name string) error {
	p, err := Find(id)
	if err != nil {
		return err
	}
	i, ok := findKey(p, keyRef)
	if !ok {
		return fmt.Errorf("%s has no such key", p.Name)
	}
	name = strings.TrimSpace(name)
	if i < 0 {
		p.KeyName = name
	} else {
		p.Keys[i].Name = name
	}
	return Save(*p)
}
