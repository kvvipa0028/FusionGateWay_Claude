package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/fusion/store"
)

var errCursor = errors.New("event cursor ahead of durable sequence")
var errEventCapacity = errors.New("event stream capacity reached")
var errStreamTransport = errors.New("event transport cannot enforce write deadline")

func cursor(r *http.Request) (int64, error) {
	values := r.Header.Values("Last-Event-ID")
	if len(values) == 0 {
		return 0, nil
	}
	if len(values) != 1 {
		return 0, errInvalid
	}
	value := values[0]
	if value == "" || len(value) > 19 || len(value) > 1 && value[0] == '0' {
		return 0, errInvalid
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, errInvalid
		}
	}
	n, e := strconv.ParseInt(value, 10, 64)
	if e != nil {
		return 0, errInvalid
	}
	return n, nil
}
func eventLine(v store.Event) ([]byte, error) {
	if v.Seq < 1 || v.Generation < 0 || !opaque(v.TaskID) || v.RunID != "" && !opaque(v.RunID) || len(v.Kind) == 0 || len(v.Kind) > 64 {
		return nil, errInvalid
	}
	for _, c := range v.Kind {
		if c != '_' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return nil, errInvalid
		}
	}
	data, e := json.Marshal(v)
	if e != nil {
		return nil, errInvalid
	}
	return []byte(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", v.Seq, v.Kind, data)), nil
}
func (s *Server) eventPage(id string, after int64) ([]store.Event, error) {
	page, e := s.store.EventsPage(id, after, 256)
	if errors.Is(e, store.ErrConflict) {
		return nil, errCursor
	}
	return page, e
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/agent/v1/tasks/"), "/events")
	if !opaque(id) {
		failure(w, errInvalid)
		return
	}
	after, e := cursor(r)
	if e != nil {
		failure(w, e)
		return
	}
	select {
	case s.eventSlots <- struct{}{}:
		defer func() { <-s.eventSlots }()
	default:
		failure(w, errEventCapacity)
		return
	}
	page, e := s.eventPage(id, after)
	if e != nil {
		failure(w, e)
		return
	}
	if !s.auth.ManagementCurrent(r.Context()) {
		http.Error(w, "Unauthorized", 401)
		return
	}
	// Validate stored metadata before opening the response; do not turn a corrupt
	// kind into injected SSE fields or accept an event with a missing sequence.
	for _, v := range page {
		if _, e = eventLine(v); e != nil {
			failure(w, e)
			return
		}
	}
	controller := http.NewResponseController(w)
	if e := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); e != nil {
		failure(w, errStreamTransport)
		return
	}
	write := func(raw []byte) bool {
		if !s.auth.ManagementCurrent(r.Context()) || r.Context().Err() != nil {
			return false
		}
		if e := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); e != nil {
			return false
		}
		if _, e := w.Write(raw); e != nil {
			return false
		}
		return controller.Flush() == nil
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	if !write([]byte(": connected\n\n")) {
		return
	}
	ticker := time.NewTicker(s.eventPoll)
	defer ticker.Stop()
	lifetime := time.NewTimer(5 * time.Minute)
	defer lifetime.Stop()
	for {
		for _, v := range page {
			raw, e := eventLine(v)
			if e != nil || !write(raw) {
				return
			}
			after = v.Seq
		}
		select {
		case <-r.Context().Done():
			return
		case <-lifetime.C:
			return
		case <-ticker.C:
		}
		if !s.auth.ManagementCurrent(r.Context()) {
			return
		}
		page, e = s.eventPage(id, after)
		if e != nil {
			return
		}
		if len(page) == 0 {
			if !write([]byte(": keepalive\n\n")) {
				return
			}
		}
	}
}
