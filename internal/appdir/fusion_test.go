//go:build fusion

package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFusionDirectoriesDoNotUseMagpieState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(os.Getenv("HOME"), "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(os.Getenv("HOME"), "cache"))
	UseExecutable("")
	t.Cleanup(func() { UseExecutable("") })
	if got, want := Config(), filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fusion-gateway"); got != want {
		t.Fatalf("config = %q, want independent %q", got, want)
	}
	if got, want := Cache(), filepath.Join(os.Getenv("XDG_CACHE_HOME"), "fusion-gateway"); got != want {
		t.Fatalf("cache = %q, want independent %q", got, want)
	}
}

func TestFusionPortableDoesNotAdoptMagpieMarkers(t *testing.T) {
	r, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(r, "fusion-gateway")
	if err := os.WriteFile(exe, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(r, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r, ".portable"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Resolve(exe); got != "" {
		t.Fatalf("adopted Magpie portable data: %q", got)
	}
	if err := os.Mkdir(filepath.Join(r, "fusion-data"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, want := Resolve(exe), filepath.Join(r, "fusion-data"); got != want {
		t.Fatalf("portable = %q, want %q", got, want)
	}
}
