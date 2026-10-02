// Package stats counts magpie's users: once a day a running magpie tells
// PostHog it is in use, under an id made up on this computer, with its
// version and system. Nothing else goes: no names, accounts, keys, models,
// prompts or usage. Settings → Privacy turns it off, and so do
// DO_NOT_TRACK=1 and MAGPIE_NO_STATS=1; a build from source never sends it.
package stats

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/update"
)

// Key is the Magpie project's key on PostHog: it can send events there and
// read nothing, so it is safe in the app.
const Key = "phc_nyDyYyrUTRGvNq5U7FBesCyM2j4GdKeqGp8ko7DBerTC"

// Host takes the events; MAGPIE_STATS_HOST points them elsewhere, for
// testing.
func Host() string {
	if h := os.Getenv("MAGPIE_STATS_HOST"); h != "" {
		return strings.TrimRight(h, "/")
	}
	return "https://us.i.posthog.com"
}

// Off reports whether the user said not to send it.
func Off() bool {
	for _, k := range []string{"DO_NOT_TRACK", "MAGPIE_NO_STATS"} {
		if v := os.Getenv(k); v != "" && v != "0" && v != "false" {
			return true
		}
	}
	return settings.Load().NoStats
}

// Run sends the day's event while magpie runs, as what ("app", "serve"): at
// start, and then whenever a new day (UTC) has begun. Only releases send
// it, unless MAGPIE_STATS_HOST is set.
func Run(version, what string) {
	if !update.Released(version) && os.Getenv("MAGPIE_STATS_HOST") == "" {
		return
	}
	for {
		// no network, most likely, when it fails: the next look tries again
		_ = Send(context.Background(), version, what, time.Now())
		time.Sleep(time.Hour)
	}
}

// Send sends the event for now's day, unless it went already or the user
// turned it off.
func Send(ctx context.Context, version, what string, now time.Time) error {
	if Off() {
		return nil
	}
	day := now.UTC().Format("2006-01-02")
	stamp := filepath.Join(settings.Dir(), "stats-sent")
	if b, _ := os.ReadFile(stamp); strings.TrimSpace(string(b)) == day {
		return nil
	}
	id, err := installID()
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{
		"api_key":     Key,
		"event":       "magpie active",
		"distinct_id": id,
		"timestamp":   now.UTC().Format(time.RFC3339),
		"properties": map[string]any{
			"version": version,
			"kind":    what,
			"$os":     osName(),
			"arch":    runtime.GOARCH,
			// a count, not a person: PostHog keeps no profile for the id
			"$process_person_profile": false,
		},
	})
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", Host()+"/i/v0/e/", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("stats: %s", res.Status)
	}
	return os.WriteFile(stamp, []byte(day+"\n"), 0o644)
}

// installID is this computer's id, made up the first time: random, tied to
// nothing about it or the user.
func installID() (string, error) {
	p := filepath.Join(settings.Dir(), "install-id")
	if b, err := os.ReadFile(p); err == nil {
		if id := strings.TrimSpace(string(b)); len(id) == 32 {
			return id, nil
		}
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return id, os.WriteFile(p, []byte(id+"\n"), 0o644)
}

func osName() string {
	switch runtime.GOOS {
	case "darwin":
		return "Mac OS X"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	}
	return runtime.GOOS
}
