package runtime

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

const ClaudeCLIVersion = "2.1.287"

var claudeModel = regexp.MustCompile(`^glm-[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

// ClaudeChannel is immutable trusted launch configuration, not an HTTP DTO.
// The controller owns the listener and its admitted Handler/Transport; this
// Both loopback families are held on one port because Seatbelt's localhost
// rule includes both. The Native process receives no upstream credential.
type ClaudeChannel struct {
	endpoint, port string
	grant          *policy.PendingModelGrant
	target         stageplan.ExecutionTarget
	mu             sync.Mutex
	active, closed bool
	server         *http.Server
	listeners      []net.Listener
	failure        chan struct{}
	failOnce       sync.Once
	connections    chan struct{}
	closing        chan struct{}
}

func (*ClaudeChannel) String() string   { return "scoped Claude channel (redacted)" }
func (*ClaudeChannel) GoString() string { return "ClaudeChannel(<redacted>)" }

// NewClaudeChannel takes only a trusted, authenticated controller handler.
// Callers cannot supply an endpoint belonging to another local service.
func NewClaudeChannel(handler http.Handler, grant *policy.PendingModelGrant, target stageplan.ExecutionTarget) (*ClaudeChannel, error) {
	if handler == nil || grant == nil {
		return nil, ErrLaunch
	}
	if !claudeModel.MatchString(target.RequestedModel) || target.RequestedModel != target.ResolvedModel || target.RuntimeVersion != ClaudeCLIVersion || target.BillingPath != "coding_plan" || target.LockEnforcement != stageplan.ControlledCalls || target.Account == "" || target.Workspace == "" || target.CredentialIdentity == "" || target.Route.ID == "" || target.Route.Revision < 1 || target.PluginVersion != nil {
		return nil, ErrLaunch
	}
	raw, e := json.Marshal(target)
	if e != nil || len(raw) > 64<<10 {
		return nil, ErrLaunch
	}
	var frozen stageplan.ExecutionTarget
	if json.Unmarshal(raw, &frozen) != nil {
		return nil, ErrLaunch
	}
	c := &ClaudeChannel{grant: grant, target: frozen, failure: make(chan struct{}), connections: make(chan struct{}, 16), closing: make(chan struct{})}
	for attempt := 0; attempt < 8; attempt++ {
		four, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return nil, ErrLaunch
		}
		port := four.Addr().(*net.TCPAddr).Port
		six, err := net.Listen("tcp6", net.JoinHostPort("::1", strconv.Itoa(port)))
		if err != nil || port < 1024 {
			four.Close()
			if six != nil {
				six.Close()
			}
			continue
		}
		c.port = strconv.Itoa(port)
		c.endpoint = "http://127.0.0.1:" + c.port + "/api/anthropic"
		c.listeners = []net.Listener{four, six}
		break
	}
	if len(c.listeners) != 2 {
		return nil, ErrLaunch
	}
	c.server = &http.Server{Handler: handler, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 70 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10, ErrorLog: log.New(io.Discard, "", 0)}
	for _, listener := range c.listeners {
		go func(l net.Listener) {
			if e := c.server.Serve(&channelListener{Listener: l, channel: c}); e != nil && e != http.ErrServerClosed {
				c.cancel()
				c.failOnce.Do(func() { close(c.failure) })
			}
		}(listener)
	}
	return c, nil
}

func (c *ClaudeChannel) valid(r store.StageRun, project string, lifetime time.Duration) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.validLocked(r, project, lifetime)
}

func (c *ClaudeChannel) validLocked(r store.StageRun, project string, lifetime time.Duration) bool {
	if c.closed || c.active {
		return false
	}
	select {
	case <-c.failure:
		return false
	default:
	}
	expected := policy.Claims{TaskID: r.TaskID, RunID: r.ID, Role: r.Role, Attempt: r.Attempt, PlanRevision: r.PlanRevision, Generation: r.Generation, ProjectID: project, Audience: policy.ModelAudience}
	a, e := json.Marshal(c.target)
	b, f := json.Marshal(r.Target)
	return e == nil && f == nil && string(a) == string(b) && c.grant.PreparedFor(expected, lifetime)
}

func (c *ClaudeChannel) acquire(r store.StageRun, project string, lifetime time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.validLocked(r, project, lifetime) {
		return false
	}
	c.active = true
	return true
}

// Close refuses to relinquish allowed ports while a managed process is alive.
// Supervisor releases them only after Wait has reaped that process.
func (c *ClaudeChannel) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active {
		return ErrLaunch
	}
	c.closeLocked()
	return nil
}

func (c *ClaudeChannel) closeLocked() {
	if c.closed {
		return
	}
	c.closed = true
	c.cancel()
	close(c.closing)
	c.server.Close()
	for _, l := range c.listeners {
		l.Close()
	}
}

func (c *ClaudeChannel) release() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active = false
	c.closeLocked()
}

type channelListener struct {
	net.Listener
	channel *ClaudeChannel
}

func (l *channelListener) Accept() (net.Conn, error) {
	select {
	case <-l.channel.closing:
		return nil, net.ErrClosed
	case l.channel.connections <- struct{}{}:
	}
	conn, e := l.Listener.Accept()
	if e != nil {
		<-l.channel.connections
		return nil, e
	}
	return &channelConnection{Conn: conn, slots: l.channel.connections}, nil
}

type channelConnection struct {
	net.Conn
	once  sync.Once
	slots chan struct{}
}

func (c *channelConnection) Close() error {
	e := c.Conn.Close()
	c.once.Do(func() { <-c.slots })
	return e
}

func (c *ClaudeChannel) cancel() {
	if c != nil {
		c.grant.Cancel()
	}
}
