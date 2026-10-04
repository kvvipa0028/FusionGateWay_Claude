package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"
)

type rpcReply struct {
	data json.RawMessage
	err  error
}

// StdioPeer owns a private duplex stream for one managed native process. Close
// revokes this transport; only the supervisor can prove OS process exit.
type StdioPeer struct {
	stream        io.ReadWriteCloser
	current       func() bool
	mu            sync.Mutex
	writeMu       sync.Mutex
	pending       map[uint64]chan rpcReply
	seen          map[uint64]bool
	done          chan struct{}
	closeOnce     sync.Once
	notifications chan []byte
}

func NewStdioPeer(stream io.ReadWriteCloser, current func() bool) (*StdioPeer, error) {
	if stream == nil || current == nil {
		return nil, ErrIdentity
	}
	p := &StdioPeer{stream: stream, current: current, pending: map[uint64]chan rpcReply{}, seen: map[uint64]bool{}, done: make(chan struct{}), notifications: make(chan []byte, 64)}
	go p.read()
	return p, nil
}
func (p *StdioPeer) Close() error {
	var e error
	p.closeOnce.Do(func() {
		close(p.done)
		e = p.stream.Close()
		p.mu.Lock()
		for _, ch := range p.pending {
			ch <- rpcReply{err: ErrNative}
		}
		p.pending = map[uint64]chan rpcReply{}
		p.mu.Unlock()
	})
	return e
}
func (p *StdioPeer) Notifications() <-chan []byte { return p.notifications }
func (p *StdioPeer) write(ctx context.Context, v any) error {
	if !p.current() {
		return ErrIdentity
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	raw, e := json.Marshal(v)
	if e != nil || len(raw) > 1<<20 {
		return ErrProtocol
	}
	raw = append(raw, '\n')
	stop := context.AfterFunc(ctx, func() { p.Close() })
	defer stop()
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	select {
	case <-p.done:
		return ErrNative
	default:
	}
	for len(raw) > 0 {
		n, e := p.stream.Write(raw)
		if e != nil || n <= 0 {
			p.Close()
			return ErrNative
		}
		raw = raw[n:]
	}
	return nil
}
func (p *StdioPeer) Call(ctx context.Context, id uint64, method string, params json.RawMessage) (json.RawMessage, error) {
	if !p.current() {
		return nil, ErrIdentity
	}
	if !allowedMethod(method) || id == 0 || id > 1<<53-1 {
		return nil, ErrProtocol
	}
	var v any
	if decode(params, &v) != nil {
		return nil, ErrProtocol
	}
	ch := make(chan rpcReply, 1)
	p.mu.Lock()
	select {
	case <-p.done:
		p.mu.Unlock()
		return nil, ErrNative
	default:
	}
	if p.seen[id] || len(p.seen) >= 4096 || len(p.pending) >= 8 {
		p.mu.Unlock()
		return nil, ErrProtocol
	}
	p.seen[id] = true
	p.pending[id] = ch
	p.mu.Unlock()
	if e := p.write(ctx, map[string]any{"id": id, "method": method, "params": params}); e != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, e
	}
	select {
	case <-ctx.Done():
		p.Close()
		return nil, ctx.Err()
	case reply := <-ch:
		return p.reply(reply)
	case <-p.done:
		// read delivers a validated response before observing EOF. Preserve
		// that response even when stdout closes before Call is scheduled.
		select {
		case reply := <-ch:
			return p.reply(reply)
		default:
		}
		return nil, ErrNative
	}
}
func (p *StdioPeer) reply(r rpcReply) (json.RawMessage, error) {
	if !p.current() {
		p.Close()
		return nil, ErrIdentity
	}
	return r.data, r.err
}
func (p *StdioPeer) Notify(ctx context.Context, method string, params json.RawMessage) error {
	if method != "initialized" {
		return ErrProtocol
	}
	var v any
	if decode(params, &v) != nil {
		return ErrProtocol
	}
	return p.write(ctx, map[string]any{"method": method, "params": params})
}
func allowedMethod(m string) bool {
	switch m {
	case "initialize", "account/read", "model/list", "account/rateLimits/read", "thread/start", "thread/resume", "turn/start", "turn/interrupt":
		return true
	}
	return false
}
func (p *StdioPeer) read() {
	defer close(p.notifications)
	defer p.Close()
	scanner := bufio.NewScanner(p.stream)
	scanner.Buffer(make([]byte, 4096), (1<<20)+1)
	total := 0
	for scanner.Scan() {
		raw := append([]byte(nil), scanner.Bytes()...)
		total += len(raw)
		if total > 8<<20 || !p.current() {
			return
		}
		var f map[string]json.RawMessage
		if decode(raw, &f) != nil {
			return
		}
		for k := range f {
			switch k {
			case "jsonrpc", "id", "method", "params", "result", "error", "emittedAtMs":
			default:
				return
			}
		}
		if j, ok := f["jsonrpc"]; ok && !bytes.Equal(j, []byte(`"2.0"`)) {
			return
		}
		if timestamp, ok := f["emittedAtMs"]; ok {
			// Pinned ServerNotificationEnvelope is an optional int64, emitted
			// only on notifications. It is observational metadata, never a
			// response ID, generation/ordering fence or admission timestamp.
			var value int64
			if f["method"] == nil || f["id"] != nil || bytes.Equal(timestamp, []byte("null")) || json.Unmarshal(timestamp, &value) != nil {
				return
			}
		}
		if m, ok := f["method"]; ok {
			var method string
			if json.Unmarshal(m, &method) != nil || method == "" || f["result"] != nil || f["error"] != nil {
				return
			}
			if id, request := f["id"]; request {
				if !validServerID(id) || p.reject(id, method) != nil {
					return
				}
				continue
			}
			if f["params"] == nil {
				return
			}
			select {
			case p.notifications <- raw:
			case <-p.done:
				return
			default:
				return
			}
			continue
		}
		var id uint64
		if json.Unmarshal(f["id"], &id) != nil || id == 0 {
			return
		}
		result, hasResult := f["result"]
		_, hasError := f["error"]
		if hasResult == hasError || f["params"] != nil {
			return
		}
		p.mu.Lock()
		ch, ok := p.pending[id]
		if ok {
			delete(p.pending, id)
		}
		p.mu.Unlock()
		if !ok {
			return
		}
		if hasError {
			ch <- rpcReply{err: ErrNative}
		} else {
			ch <- rpcReply{data: append(json.RawMessage(nil), result...)}
		}
	}
}
func validServerID(raw json.RawMessage) bool {
	var n uint64
	if json.Unmarshal(raw, &n) == nil {
		return true
	}
	var s string
	return json.Unmarshal(raw, &s) == nil && s != "" && len(s) <= 256
}
func (p *StdioPeer) reject(id json.RawMessage, method string) error {
	response := map[string]any{"id": id}
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		response["result"] = map[string]string{"decision": "cancel"}
	case "item/permissions/requestApproval":
		response["result"] = map[string]any{"permissions": map[string]any{}, "scope": "turn"}
	default:
		response["error"] = map[string]any{"code": -32601, "message": "unsupported native server request"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return p.write(ctx, response)
}

var _ Peer = (*StdioPeer)(nil)
