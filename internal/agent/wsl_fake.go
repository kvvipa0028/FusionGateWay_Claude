package agent

import (
	"errors"
	"strings"
	"time"
)

// FakeWSL, for other packages' tests, stands in for wsl.exe on any
// system: each distro named in probes is installed and running, answers
// the probe with what probes has for it (wslProbeScript's output: "home:/
// home/me\ndir:.codex\n…") and is opened at roots' folder for it. What
// magpie remembers of WSL is forgotten, before and after; the returned
// func puts everything back.
func FakeWSL(probes, roots map[string]string) func() {
	on, run, root := wslOn, wslRun, wslRoot
	reset := func() {
		wsl.Lock()
		wsl.seen, wsl.at, wsl.names, wsl.running, wsl.probed, wsl.failed, wsl.dirty = nil, time.Time{}, nil, nil, nil, nil, false
		wsl.Unlock()
	}
	reset()
	var names []string
	for n := range probes {
		names = append(names, n)
	}
	list := []byte(strings.Join(names, "\n"))
	wslOn = true
	wslRun = func(_ time.Duration, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "-l -q", "-l --running -q":
			return list, nil
		}
		if len(args) > 2 && args[0] == "-d" {
			if p, ok := probes[args[1]]; ok {
				return []byte(p), nil
			}
		}
		return nil, errors.New("unexpected wsl.exe " + strings.Join(args, " "))
	}
	wslRoot = func(name string) string { return roots[name] }
	return func() {
		wslOn, wslRun, wslRoot = on, run, root
		reset()
	}
}
