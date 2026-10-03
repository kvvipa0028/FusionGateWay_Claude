package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func submittedTask(t *testing.T, h http.Handler) store.Task {
	t.Helper()
	p := preview(t, h)
	w := submit(t, h, p, "fixture-key")
	if w.Code != 201 {
		t.Fatal(w.Code)
	}
	var task store.Task
	json.Unmarshal(w.Body.Bytes(), &task)
	return task
}
func revise(t *testing.T, st *store.Store, id string, rev int64) {
	t.Helper()
	c := configuration()
	p, e := stageplan.Compile(rev+1, []stageplan.Role{stageplan.Design}, c.Global, c.Project, stageplan.Layer{}, c.Routes)
	if e != nil {
		t.Fatal(e)
	}
	if e = st.RevisePlan(id, rev, p); e != nil {
		t.Fatal(e)
	}
}
func stream(t *testing.T, server *httptest.Server, id, cursor string) (*http.Response, *bufio.Scanner) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/agent/v1/tasks/"+id+"/events", nil)
	req.Header.Set("Authorization", "Bearer fixture-management")
	if cursor != "" {
		req.Header.Set("Last-Event-ID", cursor)
	}
	response, e := server.Client().Do(req)
	if e != nil {
		cancel()
		t.Fatal(e)
	}
	t.Cleanup(func() { response.Body.Close(); cancel() })
	return response, bufio.NewScanner(response.Body)
}
func readEvent(t *testing.T, scanner *bufio.Scanner) store.Event {
	t.Helper()
	id := ""
	var event store.Event
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id: ") {
			id = strings.TrimPrefix(line, "id: ")
		}
		if strings.HasPrefix(line, "data: ") {
			if e := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); e != nil {
				t.Fatal(e)
			}
		}
		if line == "" && event.Seq != 0 {
			if id != fmt.Sprint(event.Seq) {
				t.Fatal("SSE ID not persistent sequence")
			}
			return event
		}
	}
	t.Fatal("event unavailable", scanner.Err())
	return event
}
func TestSSEReconnectOnlyReadsPersistentEvents(t *testing.T) {
	s, st, h := setup(t)
	s.eventPoll = 10 * time.Millisecond
	task := submittedTask(t, h)
	revise(t, st, task.ID, 1)
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	response, scanner := stream(t, server, task.ID, "1")
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal(response.StatusCode, response.Header)
	}
	event := readEvent(t, scanner)
	if event.Seq != 2 || event.Kind != "plan_revised" {
		t.Fatal("wrong event replay", event)
	}
	response.Body.Close()
	response, scanner = stream(t, server, task.ID, "2")
	revise(t, st, task.ID, 2)
	event = readEvent(t, scanner)
	if event.Seq != 3 {
		t.Fatal("reconnect replayed previous event", event)
	}
	response.Body.Close()
	events, e := st.Events(task.ID, 0)
	if e != nil || len(events) != 3 {
		t.Fatal("SSE created task work", events, e)
	}
	got, e := st.Task(task.ID)
	if e != nil || got.Generation != 0 {
		t.Fatal("SSE started execution")
	}
}
func TestSSEDisconnectDoesNotCancelRunningAttempt(t *testing.T) {
	s, st, h := setup(t)
	s.eventPoll = 10 * time.Millisecond
	task := submittedTask(t, h)
	p, e := st.Plan(task.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	run, e := st.StartReserved(store.StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Owner: "fixture-worker", TTL: time.Minute, Target: *p.Bindings[stageplan.Design].Target}, store.ReservationRequest{PoolKey: "fixture-pool", AdmissionHash: strings.Repeat("a", 64), GlobalLimit: 2})
	if e != nil {
		t.Fatal(e)
	}
	if e = st.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	before, e := st.Events(task.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	response, scanner := stream(t, server, task.ID, "")
	readEvent(t, scanner)
	response.Body.Close()
	after, e := st.Events(task.ID, 0)
	if e != nil || len(after) != len(before) {
		t.Fatal("disconnect mutated execution", after, e)
	}
	got, e := st.Run(run.ID)
	if e != nil || got.State != "running" {
		t.Fatal("disconnect cancelled stage", got, e)
	}
}
func TestSSERevocationClosesLiveConnectionBeforeNewEvent(t *testing.T) {
	s, st, h := setup(t)
	s.eventPoll = 10 * time.Millisecond
	task := submittedTask(t, h)
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	response, scanner := stream(t, server, task.ID, "")
	readEvent(t, scanner)
	s.auth.RevokeManagement()
	revise(t, st, task.ID, 1)
	start := time.Now()
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "id: ") {
			t.Fatal("revoked connection received new event")
		}
	}
	if time.Since(start) > time.Second || scanner.Err() != nil {
		t.Fatal("revoked stream did not close promptly", scanner.Err())
	}
	response.Body.Close()
}
func TestSSERejectsInvalidUnknownAndAheadCursors(t *testing.T) {
	_, _, h := setup(t)
	task := submittedTask(t, h)
	for _, cursor := range []string{"-1", "+1", "01", "1,2", "9223372036854775808"} {
		req := httptest.NewRequest("GET", "/agent/v1/tasks/"+task.ID+"/events", nil)
		req.Header.Set("Authorization", "Bearer fixture-management")
		req.Header.Set("Last-Event-ID", cursor)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatal("invalid event cursor accepted", cursor, w.Code)
		}
	}
	req := httptest.NewRequest("GET", "/agent/v1/tasks/"+task.ID+"/events", nil)
	req.Header.Set("Authorization", "Bearer fixture-management")
	req.Header.Set("Last-Event-ID", "2")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 409 {
		t.Fatal("ahead cursor accepted", w.Code)
	}
	w = request(h, "GET", "/agent/v1/tasks/fixture-missing/events", "", "", "fixture-management")
	if w.Code != 404 {
		t.Fatal("unknown task stream opened", w.Code)
	}
}
func TestSSERequiresManagementAndNeverAcceptsQueryCredentials(t *testing.T) {
	_, _, h := setup(t)
	task := submittedTask(t, h)
	for _, secret := range []string{"", "fgs_fixture-stage", "fixture-wrong"} {
		w := request(h, "GET", "/agent/v1/tasks/"+task.ID+"/events", "", "", secret)
		if w.Code != 401 && w.Code != 403 {
			t.Fatal("SSE missing auth", w.Code)
		}
	}
	if w := request(h, "GET", "/agent/v1/tasks/"+task.ID+"/events?token=fixture-management", "", "", "fixture-management"); w.Code != 403 {
		t.Fatal("query auth accepted")
	}
}

func TestSSEConnectionCapacityIsBoundedAndReleasedOnDisconnect(t *testing.T) {
	s, _, h := setup(t)
	s.eventPoll = 10 * time.Millisecond
	task := submittedTask(t, h)
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	responses := make([]*http.Response, 0, 8)
	for i := 0; i < 8; i++ {
		r, _ := stream(t, server, task.ID, "")
		if r.StatusCode != 200 {
			t.Fatal("capacity too small", i, r.StatusCode)
		}
		responses = append(responses, r)
	}
	ninth, _ := stream(t, server, task.ID, "")
	if ninth.StatusCode != 429 {
		t.Fatal("unbounded event connections", ninth.StatusCode)
	}
	ninth.Body.Close()
	responses[0].Body.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r, scanner := stream(t, server, task.ID, "")
		if r.StatusCode == 200 {
			readEvent(t, scanner)
			r.Body.Close()
			return
		}
		r.Body.Close()
		if r.StatusCode != 429 {
			t.Fatal("bad post-disconnect status", r.StatusCode)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("disconnected stream retained capacity")
}
func TestSSEEventFieldsCannotInjectControlLines(t *testing.T) {
	for _, kind := range []string{"created\nid: 999", "Created", "created\rdata: forged"} {
		if _, e := eventLine(store.Event{TaskID: "fixture-task", Seq: 1, Kind: kind}); e == nil {
			t.Fatal("unsafe event kind encoded")
		}
	}
	if _, e := eventLine(store.Event{TaskID: "fixture-task\n", Seq: 1, Kind: "created"}); e == nil {
		t.Fatal("unsafe metadata encoded")
	}
}

func TestSSEUnsupportedTransportCannotOpenAnUnboundedStream(t *testing.T) {
	_, _, h := setup(t)
	task := submittedTask(t, h)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("GET", "/agent/v1/tasks/"+task.ID+"/events", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer fixture-management")
	// Recorder can flush but cannot enforce a network write deadline. It is a
	// positive authorization control for the unsupported-transport boundary.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 503 || strings.Contains(w.Body.String(), "id: ") {
		t.Fatal("deadline-unavailable transport streamed task data", w.Code)
	}
}
