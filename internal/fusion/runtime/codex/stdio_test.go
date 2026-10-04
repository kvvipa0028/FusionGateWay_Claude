package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func pipePeer(t *testing.T) (*StdioPeer, net.Conn, *atomic.Bool) {
	t.Helper()
	a, b := net.Pipe()
	current := &atomic.Bool{}
	current.Store(true)
	p, e := NewStdioPeer(a, current.Load)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Close(); b.Close() })
	return p, b, current
}
func readFrame(t *testing.T, r *bufio.Reader) map[string]json.RawMessage {
	t.Helper()
	b, e := r.ReadBytes('\n')
	if e != nil {
		t.Error(e)
		return nil
	}
	var f map[string]json.RawMessage
	if json.Unmarshal(b, &f) != nil {
		t.Error("invalid request")
	}
	return f
}
func TestStdioCorrelatesResponseAndSeparatesNotifications(t *testing.T) {
	p, b, _ := pipePeer(t)
	go func() {
		r := bufio.NewReader(b)
		f := readFrame(t, r)
		if string(f["id"]) != "1" {
			t.Error("id not preserved")
		}
		b.Write([]byte(`{"method":"thread/started","params":{"thread":{"id":"fixture-thread"}}}` + "\n"))
		b.Write([]byte(`{"id":1,"result":{"fixture":"ok"}}` + "\n"))
	}()
	ctx, c := context.WithTimeout(context.Background(), time.Second)
	defer c()
	raw, e := p.Call(ctx, 1, "initialize", json.RawMessage(`{}`))
	if e != nil || string(raw) != `{"fixture":"ok"}` {
		t.Fatal(e, string(raw))
	}
	select {
	case f := <-p.Notifications():
		if len(f) == 0 {
			t.Fatal("notification lost")
		}
	case <-ctx.Done():
		t.Fatal("notification timeout")
	}
}

func TestStdioPinnedNotificationTimestamp(t *testing.T) {
	for _, stamp := range []string{"0", "1234", "9223372036854775807", "-9223372036854775808", "null", "-1", "1.5", `"1234"`, "9223372036854775808", "-9223372036854775809", "{}"} {
		t.Run(stamp, func(t *testing.T) {
			p, b, _ := pipePeer(t)
			go func() {
				readFrame(t, bufio.NewReader(b))
				b.Write([]byte(`{"method":"remoteControl/status/changed","params":{},"emittedAtMs":` + stamp + "}\n"))
				b.Write([]byte("{\"id\":1,\"result\":{}}\n"))
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, e := p.Call(ctx, 1, "initialize", json.RawMessage(`{}`))
			allowed := stamp == "0" || stamp == "1234" || stamp == "9223372036854775807" || stamp == "-9223372036854775808" || stamp == "-1"
			if (e == nil) != allowed {
				t.Fatal("pinned timestamp contract", e)
			}
			if allowed {
				select {
				case frame := <-p.Notifications():
					if !strings.Contains(string(frame), `"emittedAtMs":`+stamp) {
						t.Fatal("timestamp lost")
					}
				case <-ctx.Done():
					t.Fatal("notification missing")
				}
			}
		})
	}
	for _, frame := range []string{`{"id":1,"result":{},"emittedAtMs":1}`, `{"id":2,"method":"item/fileChange/requestApproval","params":{},"emittedAtMs":1}`, `{"method":"thread/started","params":{},"emittedAtMs":1,"EmittedAtMs":2}`, `{"method":"thread/started","params":{},"emittedAtMs":1,"extra":2}`} {
		p, b, _ := pipePeer(t)
		go func() { readFrame(t, bufio.NewReader(b)); b.Write([]byte(frame + "\n")) }()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if _, e := p.Call(ctx, 1, "initialize", json.RawMessage(`{}`)); e == nil {
			t.Fatal("timestamp widened another envelope")
		}
		cancel()
	}
}
func TestStdioDeclinesServerPermissionRequests(t *testing.T) {
	p, b, _ := pipePeer(t)
	go func() {
		r := bufio.NewReader(b)
		readFrame(t, r)
		b.Write([]byte(`{"id":"fixture-request","method":"item/commandExecution/requestApproval","params":{}}` + "\n"))
		f := readFrame(t, r)
		if string(f["id"]) != `"fixture-request"` || string(f["result"]) != `{"decision":"cancel"}` {
			t.Error("server authority granted")
		}
		b.Write([]byte(`{"id":1,"result":{}}` + "\n"))
	}()
	ctx, c := context.WithTimeout(context.Background(), time.Second)
	defer c()
	if _, e := p.Call(ctx, 1, "initialize", json.RawMessage(`{}`)); e != nil {
		t.Fatal(e)
	}
}
func TestStdioBadCorrelationOrDuplicateReplyClosesTransport(t *testing.T) {
	for _, reply := range []string{`{"id":2,"result":{}}`, `{"id":1,"result":{},"result":{"secret":"fixture"}}`, `{"id":1,"error":{"message":"fixture secret"}}`} {
		t.Run(reply, func(t *testing.T) {
			p, b, _ := pipePeer(t)
			go func() { readFrame(t, bufio.NewReader(b)); b.Write([]byte(reply + "\n")) }()
			ctx, c := context.WithTimeout(context.Background(), time.Second)
			defer c()
			if _, e := p.Call(ctx, 1, "initialize", json.RawMessage(`{}`)); e == nil || e.Error() == "fixture secret" {
				t.Fatal("invalid reply accepted")
			}
		})
	}
}
func TestStdioTimeoutMakesLostAckNonRetryable(t *testing.T) {
	p, b, _ := pipePeer(t)
	go func() { readFrame(t, bufio.NewReader(b)) }()
	ctx, c := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer c()
	if _, e := p.Call(ctx, 1, "turn/start", json.RawMessage(`{}`)); e == nil {
		t.Fatal("timeout accepted")
	}
	if _, e := p.Call(context.Background(), 2, "turn/start", json.RawMessage(`{}`)); e == nil {
		t.Fatal("lost ack replayable")
	}
}
func TestStdioRejectsWrongScopeAndManagementMethodsWithoutWrite(t *testing.T) {
	p, b, current := pipePeer(t)
	current.Store(false)
	if _, e := p.Call(context.Background(), 1, "initialize", json.RawMessage(`{}`)); !errors.Is(e, ErrIdentity) {
		t.Fatal("scope bypass")
	}
	current.Store(true)
	if _, e := p.Call(context.Background(), 2, "account/login/start", json.RawMessage(`{}`)); e == nil {
		t.Fatal("worker login allowed")
	}
	b.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	var buf [1]byte
	if n, _ := b.Read(buf[:]); n != 0 {
		t.Fatal("rejected write escaped")
	}
}

// A native process may send its final response and immediately close stdout.
// Hold Write until EOF is observed to exercise the reply/close ordering.
type finalReplyStream struct {
	started, closed chan struct{}
	once            sync.Once
	read            bool
}

func (s *finalReplyStream) Read(b []byte) (int, error) {
	<-s.started
	if s.read {
		return 0, io.EOF
	}
	s.read = true
	return copy(b, []byte("{\"id\":1,\"result\":{}}\n")), nil
}
func (s *finalReplyStream) Write(b []byte) (int, error) {
	close(s.started)
	<-s.closed
	return len(b), nil
}
func (s *finalReplyStream) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}
func TestStdioFinalResponseSurvivesImmediateEOF(t *testing.T) {
	for i := 0; i < 32; i++ {
		s := &finalReplyStream{started: make(chan struct{}), closed: make(chan struct{})}
		p, e := NewStdioPeer(s, func() bool { return true })
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		raw, e := p.Call(ctx, 1, "account/read", json.RawMessage(`{}`))
		cancel()
		p.Close()
		if e != nil || string(raw) != `{}` {
			t.Fatal("valid final response lost", e)
		}
	}
}

func TestStdioOversizeOrUndrainedNotificationsRevokeTransport(t *testing.T) {
	for _, oversize := range []bool{true, false} {
		p, b, _ := pipePeer(t)
		go func() {
			if oversize {
				b.Write([]byte(strings.Repeat("x", (1<<20)+1) + "\n"))
				return
			}
			for i := 0; i < 65; i++ {
				if _, e := b.Write([]byte("{\"method\":\"thread/started\",\"params\":{}}\n")); e != nil {
					return
				}
			}
		}()
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			t.Fatal("unbounded stream remained active")
		}
	}
}
func TestStdioUnansweredServerRequestCannotHangIdlePeer(t *testing.T) {
	p, b, _ := pipePeer(t)
	go func() { b.Write([]byte("{\"id\":1,\"method\":\"item/fileChange/requestApproval\",\"params\":{}}\n")) }()
	// The native side deliberately never reads the rejection. No client RPC
	// deadline exists, so the server-response deadline must revoke the stream.
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		t.Fatal("idle native reply write hung forever")
	}
}
