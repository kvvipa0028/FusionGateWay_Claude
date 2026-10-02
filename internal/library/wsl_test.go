package library

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/agent"
)

// wslSandbox is sandbox with a running WSL distro, Ubuntu-24.04, whose /
// is a temp folder and whose user /home/me has Codex, Claude Code, Pi and
// OmO; it gives back that user's home as magpie opens it.
func wslSandbox(t *testing.T) string {
	t.Helper()
	sandbox(t)
	root := t.TempDir()
	home := filepath.Join(root, "home", "me")
	write(t, filepath.Join(home, ".codex", "config.toml"), "model = \"gpt-5.5\"\n")
	write(t, filepath.Join(home, ".claude", "settings.json"), "{}\n")
	write(t, filepath.Join(home, ".pi", "agent", "settings.json"), "{}\n")
	write(t, filepath.Join(home, ".omo", "agent", "settings.json"), "{}\n")
	t.Cleanup(agent.FakeWSL(map[string]string{
		"Ubuntu-24.04": "home:/home/me\ndir:.codex\ndir:.claude\ndir:.pi\ndir:.omo\n",
	}, map[string]string{"Ubuntu-24.04": root}))
	return home
}

const (
	wslCodex  = "codex@wsl:Ubuntu-24.04"
	wslClaude = "claude@wsl:Ubuntu-24.04"
	wslPi     = "pi@wsl:Ubuntu-24.04"
	wslOmO    = "omo@wsl:Ubuntu-24.04"
)

// The agents magpie found in a WSL distro are the library's to give to
// (#331), each at its own files in the distro's home, not Windows'.
func TestWSLTargets(t *testing.T) {
	h := wslSandbox(t)
	want := map[string][3]string{ // instructions, MCP, skills
		wslCodex:  {".codex/AGENTS.md", ".codex/config.toml", ".codex/skills"},
		wslClaude: {".claude/CLAUDE.md", ".claude.json", ".claude/skills"},
		wslPi:     {".pi/agent/AGENTS.md", "", ".pi/agent/skills"},
		wslOmO:    {".omo/agent/AGENTS.md", ".omo/agent/mcp.json", ".omo/agent/skills"},
	}
	for id, w := range want {
		tg := targetByID(id)
		if tg == nil {
			t.Fatalf("%s isn't a target: %v", id, ids(Targets()))
		}
		mcp := ""
		if tg.MCP != nil {
			mcp = tg.MCP.Path
		}
		at := func(rel string) string {
			if rel == "" {
				return ""
			}
			return filepath.Join(h, rel)
		}
		if tg.Instructions != at(w[0]) || mcp != at(w[1]) || tg.Skills != at(w[2]) || !tg.Copy {
			t.Errorf("%s: %+v (mcp %s)", id, tg, mcp)
		}
	}
	for _, kind := range []string{"instructions", "mcp", "skills"} {
		if id, err := Takes("Codex · WSL Ubuntu-24.04", kind); err != nil || id != wslCodex {
			t.Errorf("Takes %s: %s %v", kind, id, err)
		}
	}
	// a stopped distro's is none: opening its files would start it
	if tg := targetOf(&agent.Agent{ID: "codex@wsl:Stopped", WSL: "Stopped"}); tg != nil {
		t.Errorf("stopped distro: %+v", tg)
	}
}

// A skill reaches an agent in WSL as a copy — a link to a Windows folder
// is nothing it can follow — which is copied again when the library's
// changes, and taken away with the rest.
func TestWSLSkillsCopied(t *testing.T) {
	h := wslSandbox(t)
	src := filepath.Join(home(), "src", "skills")
	skill(t, filepath.Join(src, "pdf"), "pdf", "Read PDFs")
	ok(t)(InstallSkills(src, []string{"pdf"}, []string{"claude", wslCodex, wslClaude, wslPi}))
	for _, d := range []string{".codex/skills/pdf", ".claude/skills/pdf", ".pi/agent/skills/pdf"} {
		p := filepath.Join(h, d)
		fi, err := os.Lstat(p)
		if err != nil || !fi.IsDir() {
			t.Fatalf("%s: not a folder: %v", d, err)
		}
		if !strings.Contains(read(t, filepath.Join(p, "SKILL.md")), "Read PDFs") || read(t, filepath.Join(p, "scripts/run.sh")) != "echo hi\n" || !ours(p, "pdf") {
			t.Errorf("%s isn't the library's skill", d)
		}
	}
	if !linked(filepath.Join(home(), ".claude/skills/pdf")) {
		t.Error("this machine's Claude Code got a copy, not a link")
	}
	// the skill is its folder: an edit there reaches the copies at the next sync
	write(t, filepath.Join(src, "pdf/SKILL.md"), "---\nname: pdf\ndescription: Changed\n---\n")
	res := ok(t)(Sync())
	if !slices.Contains(res.Changed, wslCodex) {
		t.Errorf("changed: %v", res.Changed)
	}
	if !strings.Contains(read(t, filepath.Join(h, ".codex/skills/pdf/SKILL.md")), "Changed") {
		t.Error("the copy wasn't made again")
	}
	if res := ok(t)(Sync()); len(res.Changed) != 0 {
		t.Errorf("a sync with nothing new wrote %v", res.Changed)
	}
	ok(t)(SkillAgents("pdf", []string{"claude", wslClaude}))
	if _, err := os.Lstat(filepath.Join(h, ".codex/skills/pdf")); !os.IsNotExist(err) {
		t.Errorf("codex@wsl still has it: %v", err)
	}
	if !ours(filepath.Join(h, ".claude/skills/pdf"), "pdf") {
		t.Error("claude@wsl lost it")
	}
}

// MCP servers are written into the distro's files in each agent's own
// format; one that runs a Windows program isn't given to them.
func TestWSLServers(t *testing.T) {
	h := wslSandbox(t)
	ok(t)(SaveServer("", Server{Name: "fetch", Transport: "stdio", Command: "uvx", Args: []string{"mcp-server-fetch"},
		Agents: []string{"codex", wslCodex, wslClaude, wslOmO}}))
	if s := read(t, filepath.Join(h, ".codex/config.toml")); !strings.Contains(s, "[mcp_servers.fetch]") || !strings.Contains(s, `command = "uvx"`) || !strings.Contains(s, `model = "gpt-5.5"`) {
		t.Errorf("codex@wsl config.toml:\n%s", s)
	}
	if s := read(t, filepath.Join(h, ".claude.json")); !strings.Contains(s, `"fetch"`) || !strings.Contains(s, `"uvx"`) {
		t.Errorf("claude@wsl .claude.json:\n%s", s)
	}
	if s := read(t, filepath.Join(h, ".omo/agent/mcp.json")); !strings.Contains(s, `"fetch"`) {
		t.Errorf("omo@wsl mcp.json:\n%s", s)
	}
	if s := read(t, filepath.Join(home(), ".codex/config.toml")); !strings.Contains(s, "[mcp_servers.fetch]") {
		t.Errorf("this machine's codex:\n%s", s)
	}

	res, err := SaveServer("", Server{Name: "win", Transport: "stdio", Command: `C:\Program Files\nodejs\npx.cmd`, Args: []string{"x"},
		Agents: []string{"codex", wslCodex}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 1 || res.Problems[0].Agent != wslCodex || res.Problems[0].What != "mcp:win" {
		t.Errorf("problems: %+v", res.Problems)
	}
	if strings.Contains(read(t, filepath.Join(h, ".codex/config.toml")), "mcp_servers.win") {
		t.Error("the Windows program was given to codex@wsl")
	}
	if !strings.Contains(read(t, filepath.Join(home(), ".codex/config.toml")), "mcp_servers.win") {
		t.Error("this machine's codex didn't get it")
	}
}

// The shared instructions, and what one WSL agent gets besides, go into
// its file in the distro; the files magpie keeps of it are named without
// the colon Windows can't have in a name.
func TestWSLInstructions(t *testing.T) {
	h := wslSandbox(t)
	write(t, filepath.Join(h, ".codex/AGENTS.md"), "Mine.\n")
	shared, extra := "Be brief.", "Use apt."
	res := ok(t)(SaveInstructions(InstructionsChange{Shared: &shared, Agents: []string{wslCodex, wslClaude},
		Extra: map[string]*string{wslCodex: &extra}}))
	s := read(t, filepath.Join(h, ".codex/AGENTS.md"))
	if !strings.HasPrefix(s, "Mine.\n") || !strings.Contains(s, "Be brief.\n\nUse apt.") {
		t.Errorf("codex@wsl AGENTS.md:\n%s", s)
	}
	if s := read(t, filepath.Join(h, ".claude/CLAUDE.md")); !strings.Contains(s, "Be brief.") || strings.Contains(s, "apt") {
		t.Errorf("claude@wsl CLAUDE.md:\n%s", s)
	}
	// what it gets besides, and its file kept before the change
	for _, dir := range []string{filepath.Dir(extraPath(wslCodex)), res.Backup} {
		es, _ := os.ReadDir(dir)
		var names []string
		for _, e := range es {
			names = append(names, e.Name())
		}
		if !slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(n, "codex@wsl") && !strings.Contains(n, ":") }) ||
			slices.ContainsFunc(names, func(n string) bool { return strings.Contains(n, ":") }) {
			t.Errorf("%s: %v", dir, names)
		}
	}
	// a setup carried elsewhere keeps what it gets besides
	if x := extras(); x[wslCodex] != extra {
		t.Errorf("extras: %v", x)
	}
	iv, err := ReadInstructions()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(iv.Agents, func(a AgentInstructions) bool { return a.Agent == wslCodex })
	if i < 0 || !iv.Agents[i].On || iv.Agents[i].Extra != extra || iv.Agents[i].Own != 1 {
		t.Errorf("instructions page: %+v", iv.Agents)
	}
}
