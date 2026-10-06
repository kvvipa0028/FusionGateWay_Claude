//go:build fusion

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// fusion-task is a local client for a serving fusion-control host. It
// discovers the loopback address and reuses the private management token from
// the isolated state root; it never starts a host, holds credentials or
// bypasses Management authorization.
func fusionTaskCommand(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "fusion-task" {
		return false, nil
	}
	return true, runFusionTask(args[1:], os.Stdout)
}

var fusionTaskUsage = `usage: fusion-task <command> [flags]

commands:
  preview  -project P -goal G [-role R]              plan a stage (no execution)
  submit   -project P -preview ID -hash H [-key K]   freeze a task
  list     -project P [-before CURSOR]               recent tasks of the project
  show     -task T                                   one task with its current ETag
  start    -task T -role R [-key K]                  start one stage of a ready task
  cancel   -task T                                   cancel the running task
`

type fusionTaskCall struct {
	method, path, etag, key string
	body                    map[string]any
	needKey                 bool
}

// fusionTaskStateRoot validates the launcher-provided isolated state root.
func fusionTaskStateRoot() (string, error) {
	root := os.Getenv("FUSION_STATE_ROOT")
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || strings.Contains(root, "..") {
		return "", fmt.Errorf("use the Fusion isolated launcher")
	}
	return root, nil
}

// readControlFile reads one fixed name inside the private control directory.
// The joined path is re-checked to stay inside that directory.
func readControlFile(root, name string) ([]byte, error) {
	control := filepath.Join(root, "data", "fusion-gateway", "control")
	path := filepath.Join(control, name)
	if !strings.HasPrefix(path, control+string(filepath.Separator)) || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return nil, fmt.Errorf("invalid control file")
	}
	return os.ReadFile(path)
}

// controlURL builds a request URL for the host-published loopback address.
// Every request goes through this single loopback-confined constructor.
func controlURL(address, path string) (string, error) {
	host, port, e := net.SplitHostPort(address)
	if e != nil || port == "" {
		return "", fmt.Errorf("control address is not a loopback endpoint")
	}
	host = strings.ToLower(host)
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return "", fmt.Errorf("control address is not a loopback endpoint")
		}
	}
	return "http://" + address + path, nil
}

func opaqueCLI(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func runFusionTask(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", strings.TrimSpace(fusionTaskUsage))
	}
	root, e := fusionTaskStateRoot()
	if e != nil {
		return e
	}
	token, e := readControlFile(root, "management.token")
	if e != nil {
		return fmt.Errorf("no serving control host; start magpie fusion-control first")
	}
	raw, e := readControlFile(root, "control.address")
	if e != nil {
		return fmt.Errorf("no serving control host; start magpie fusion-control first")
	}
	address := strings.TrimSpace(string(raw))
	if _, e := controlURL(address, ""); e != nil {
		return e
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 30 * time.Second}
	c, e := fusionTaskPlan(args, client, address, strings.TrimSpace(string(token)))
	if e != nil {
		return e
	}
	return fusionTaskCallRun(client, address, strings.TrimSpace(string(token)), c, out)
}

func fusionTaskPlan(args []string, client *http.Client, address, token string) (fusionTaskCall, error) {
	op := args[0]
	flags := flag.NewFlagSet("fusion-task "+op, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	switch op {
	case "preview":
		project := flags.String("project", "", "registered project id")
		goal := flags.String("goal", "", "task goal")
		role := flags.String("role", "design", "single required stage")
		if flags.Parse(args[1:]) != nil || !opaqueCLI(*project) || *goal == "" {
			return fusionTaskCall{}, fmt.Errorf("preview needs -project and -goal")
		}
		return fusionTaskCall{method: "POST", path: "/control/v1/tasks/preview", body: map[string]any{"project_id": *project, "goal": *goal, "required_roles": []string{*role}}}, nil
	case "submit":
		project := flags.String("project", "", "registered project id")
		preview := flags.String("preview", "", "preview id")
		hash := flags.String("hash", "", "plan hash")
		key := flags.String("key", "", "explicit idempotency key; default generated")
		if flags.Parse(args[1:]) != nil || !opaqueCLI(*project) || !opaqueCLI(*preview) || len(*hash) != 64 || strings.Trim(*hash, "0123456789abcdef") != "" || *key != "" && !opaqueCLI(*key) {
			return fusionTaskCall{}, fmt.Errorf("submit needs -project, -preview and a 64-hex -hash")
		}
		return fusionTaskCall{method: "POST", path: "/agent/v1/tasks", body: map[string]any{"preview_id": *preview, "plan_hash": *hash}, key: *key, needKey: true}, nil
	case "list":
		project := flags.String("project", "", "registered project id")
		before := flags.String("before", "", "page cursor")
		if flags.Parse(args[1:]) != nil || !opaqueCLI(*project) || *before != "" && !opaqueCLI(*before) {
			return fusionTaskCall{}, fmt.Errorf("list needs -project")
		}
		path := "/control/v1/projects/" + *project + "/tasks"
		if *before != "" {
			path += "/before/" + *before
		}
		return fusionTaskCall{method: "GET", path: path}, nil
	case "show":
		task := flags.String("task", "", "task id")
		if flags.Parse(args[1:]) != nil || !opaqueCLI(*task) {
			return fusionTaskCall{}, fmt.Errorf("show needs -task")
		}
		return fusionTaskCall{method: "GET", path: "/agent/v1/tasks/" + *task}, nil
	case "start":
		task := flags.String("task", "", "task id")
		role := flags.String("role", "", "stage to start")
		key := flags.String("key", "", "explicit idempotency key; default generated")
		if flags.Parse(args[1:]) != nil || !opaqueCLI(*task) || *role == "" || *key != "" && !opaqueCLI(*key) {
			return fusionTaskCall{}, fmt.Errorf("start needs -task and -role")
		}
		etag, e := fusionTaskETag(client, address, token, *task)
		if e != nil {
			return fusionTaskCall{}, e
		}
		return fusionTaskCall{method: "POST", path: "/control/v1/tasks/" + *task + "/start", body: map[string]any{"role": *role}, etag: etag, key: *key, needKey: true}, nil
	case "cancel":
		task := flags.String("task", "", "task id")
		if flags.Parse(args[1:]) != nil || !opaqueCLI(*task) {
			return fusionTaskCall{}, fmt.Errorf("cancel needs -task")
		}
		etag, e := fusionTaskETag(client, address, token, *task)
		if e != nil {
			return fusionTaskCall{}, e
		}
		return fusionTaskCall{method: "POST", path: "/control/v1/tasks/" + *task + "/cancel", body: map[string]any{}, etag: etag}, nil
	default:
		return fusionTaskCall{}, fmt.Errorf("unknown fusion-task command %q\n%s", op, strings.TrimSpace(fusionTaskUsage))
	}
}

func fusionTaskETag(client *http.Client, address, token, task string) (string, error) {
	url, e := controlURL(address, "/agent/v1/tasks/"+task)
	if e != nil {
		return "", e
	}
	request, e := http.NewRequest(http.MethodGet, url, nil)
	if e != nil {
		return "", e
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, e := client.Do(request)
	if e != nil {
		return "", fmt.Errorf("control host unreachable")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != 200 {
		return "", fmt.Errorf("task read failed: %s", response.Status)
	}
	// Control endpoints expose X-Fusion-Task-ETag; the agent task read
	// carries the same condition as the plain ETag.
	etag := response.Header.Get("X-Fusion-Task-ETag")
	if etag == "" {
		etag = response.Header.Get("ETag")
	}
	if etag == "" {
		return "", fmt.Errorf("task condition missing")
	}
	return etag, nil
}

func fusionTaskCallRun(client *http.Client, address, token string, c fusionTaskCall, out io.Writer) error {
	var payload []byte
	if c.body != nil {
		if c.needKey && c.key == "" {
			var entropy [16]byte
			if _, e := rand.Read(entropy[:]); e != nil {
				return e
			}
			c.key = "cli-" + hex.EncodeToString(entropy[:])
		}
		raw, e := json.Marshal(c.body)
		if e != nil {
			return e
		}
		payload = raw
	}
	url, e := controlURL(address, c.path)
	if e != nil {
		return e
	}
	request, e := http.NewRequest(c.method, url, bytes.NewReader(payload))
	if e != nil {
		return e
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.etag != "" {
		request.Header.Set("If-Match", c.etag)
	}
	if c.key != "" {
		request.Header.Set("Idempotency-Key", c.key)
	}
	response, e := client.Do(request)
	if e != nil {
		return fmt.Errorf("control host unreachable")
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", " ") == nil {
		body = pretty.Bytes()
	}
	fmt.Fprintln(out, strings.TrimSpace(string(body)))
	if response.StatusCode >= 300 {
		return fmt.Errorf("request failed: %s", response.Status)
	}
	return nil
}
