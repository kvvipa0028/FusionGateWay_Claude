package bootstrap

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/store"
)

var ErrControlHost = errors.New("Fusion local control host unavailable")

type ControlHost struct {
	mu             sync.Mutex
	closed, served bool
	once           sync.Once
	handlers       sync.WaitGroup
	source         *Loaded
	root           string
	rootIdentity   fileIdentity
	tasksIdentity  fileIdentity
	tokenIdentity  fileIdentity
	tokenDigest    [32]byte
	auth           *policy.Manager
	store          *store.Store
	server         *http.Server
	listener       net.Listener
	stop           chan struct{}
	closeErr       error
	closeDone      chan struct{}
	services       sync.WaitGroup
	runtimeCancel  context.CancelFunc
	runtimeContext context.Context
	controller     *control.Controller
	runtime        *RuntimeRegistration
}

func (*ControlHost) String() string   { return "Fusion local control host (redacted)" }
func (*ControlHost) GoString() string { return "ControlHost(<redacted>)" }
func (h *ControlHost) Addr() string {
	if h == nil {
		return ""
	}
	return h.listener.Addr().String()
}

// OpenControl exposes unadmitted draft configuration only. It does not install
// a Controller or grant Runtime, quota, workspace or route execution authority.
func OpenControl(sourcePath, root, addr string) (*ControlHost, error) {
	return openControl(nil, sourcePath, root, addr, nil)
}

func openControl(parent context.Context, sourcePath, root, addr string, factory RuntimeFactory) (*ControlHost, error) {
	host, port, e := net.SplitHostPort(addr)
	n, pe := strconv.Atoi(port)
	if e != nil || pe != nil || host != "127.0.0.1" || n < 0 || n > 65535 || strconv.Itoa(n) != port || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, ErrControlHost
	}
	source, e := Load(sourcePath)
	if e != nil {
		return nil, ErrControlHost
	}
	for _, p := range source.projects {
		if overlaps(root, p.project.Path) || overlaps(p.project.Path, root) {
			return nil, ErrControlHost
		}
	}
	identity, record, e := openControlFiles(root)
	if e != nil {
		return nil, ErrControlHost
	}
	secret := string(record.raw)
	if strings.HasSuffix(secret, "\n") {
		secret = strings.TrimSuffix(secret, "\n")
	}
	if len(secret) != 47 || !strings.HasPrefix(secret, "fgm_") || strings.ContainsFunc(secret[4:], func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	}) {
		return nil, ErrControlHost
	}
	tasksIdentity, e := readFolder(filepath.Join(root, "tasks"))
	if e != nil {
		return nil, ErrControlHost
	}
	st, e := store.Open(filepath.Join(root, "tasks"))
	if e != nil {
		return nil, ErrControlHost
	}
	listener, e := net.Listen("tcp4", addr)
	if e != nil {
		st.Close()
		return nil, ErrControlHost
	}
	auth := policy.NewManager(secret, policy.StoreValidator(st), []string{"http://" + listener.Addr().String()})
	s, e := api.New(st, auth)
	if e != nil {
		listener.Close()
		st.Close()
		return nil, ErrControlHost
	}
	for _, p := range source.Projects() {
		if e = s.SetProject(p.ID, p.Configuration); e != nil {
			listener.Close()
			st.Close()
			return nil, ErrControlHost
		}
	}
	h := &ControlHost{source: source, root: root, rootIdentity: identity, tasksIdentity: tasksIdentity, tokenIdentity: record.fileIdentity, tokenDigest: sha256.Sum256(record.raw), auth: auth, store: st, listener: listener, stop: make(chan struct{}), closeDone: make(chan struct{})}
	// Initialize closeable resources before invoking trusted factory callbacks.
	h.server = &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, ErrorLog: log.New(io.Discard, "", 0)}
	if factory != nil {
		if e = h.installRuntime(parent, s, factory); e != nil {
			h.Close()
			return nil, ErrControlHost
		}
	}
	handler := s.Handler()
	h.server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			http.Error(w, "Service Unavailable", 503)
			return
		}
		h.handlers.Add(1)
		h.mu.Unlock()
		defer h.handlers.Done()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		peer, _, e := net.SplitHostPort(r.RemoteAddr)
		if e != nil || peer != "127.0.0.1" || r.Host != h.Addr() {
			http.Error(w, "Forbidden", 403)
			return
		}
		if !h.current() {
			h.revoke()
			http.Error(w, "Service Unavailable", 503)
			return
		}
		handler.ServeHTTP(w, r)
	})
	if !h.current() || h.runtimeContext != nil && h.runtimeContext.Err() != nil {
		h.Close()
		return nil, ErrControlHost
	}
	if parent != nil {
		go func() {
			select {
			case <-parent.Done():
				h.beginClose()
			case <-h.stop:
			}
		}()
	}
	return h, nil
}
func (h *ControlHost) current() bool {
	root, e := controlRootIdentity(h.root)
	if e != nil || root != h.rootIdentity {
		return false
	}
	tasks, e := readFolder(filepath.Join(h.root, "tasks"))
	if e != nil || tasks != h.tasksIdentity {
		return false
	}
	token, e := readSource(filepath.Join(h.root, "management.token"))
	if e != nil || token.fileIdentity != h.tokenIdentity || sha256.Sum256(token.raw) != h.tokenDigest {
		return false
	}
	record, e := readSource(h.source.path)
	if e != nil || record.fileIdentity != h.source.identity || sha256.Sum256(record.raw) != h.source.digest {
		return false
	}
	for _, p := range h.source.projects {
		folder, e := readFolder(p.project.Path)
		if e != nil || folder != p.folder {
			return false
		}
	}
	return true
}
func (h *ControlHost) Serve(ctx context.Context) error {
	if h == nil || ctx == nil {
		return ErrControlHost
	}
	h.mu.Lock()
	if h.closed || h.served {
		h.mu.Unlock()
		return ErrControlHost
	}
	h.served = true
	h.mu.Unlock()
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-h.stop:
				return
			case <-ticker.C:
				if !h.current() {
					h.revoke()
				}
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- h.server.Serve(h.listener) }()
	var result error
	select {
	case <-ctx.Done():
	case e := <-done:
		if !errors.Is(e, http.ErrServerClosed) {
			result = ErrControlHost
		}
	}
	if e := h.Close(); e != nil {
		result = e
	}
	<-watchDone
	return result
}
func (h *ControlHost) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return h.CloseContext(ctx)
}

// CloseContext observes an ordered owned shutdown. A caller timeout does not
// close the database, attest stopped processes, or prevent a later wait.
func (h *ControlHost) CloseContext(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		return ErrControlHost
	}
	h.beginClose()
	select {
	case <-h.closeDone:
		return h.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (h *ControlHost) revoke() {
	h.auth.RevokeManagement()
	if h.runtimeCancel != nil {
		h.runtimeCancel()
	}
}
func (h *ControlHost) beginClose() {
	h.once.Do(func() {
		h.mu.Lock()
		h.closed = true
		h.mu.Unlock()
		close(h.stop)
		h.revoke()
		h.server.Close()
		h.listener.Close()
		go func() {
			defer close(h.closeDone)
			h.handlers.Wait()
			if h.controller != nil && h.controller.Close(context.Background()) != nil {
				h.closeErr = ErrControlHost
			}
			h.services.Wait()
			if h.runtime != nil && h.runtime.Close != nil && h.runtime.Close(context.Background()) != nil {
				h.closeErr = ErrControlHost
			}
			if h.store.Close() != nil {
				h.closeErr = ErrControlHost
			}
		}()
	})
}
