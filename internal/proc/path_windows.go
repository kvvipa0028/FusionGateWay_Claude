package proc

import (
	"os"
	"path/filepath"
)

// UserPath has nothing to do on Windows: an app started from the Start menu
// gets the user's PATH from the registry, as a terminal does.
func UserPath() {}

// UserBinDirs are the folders a user's command-line tools are installed in
// that exist here — npm's global one, bun's, volta's, pnpm's, the
// standalone installers' — for finding one PATH doesn't reach.
func UserBinDirs() []string {
	home, _ := os.UserHomeDir()
	known := []string{
		filepath.Join(os.Getenv("APPDATA"), "npm"),
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Volta", "bin"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "pnpm"),
	}
	var have []string
	for _, d := range known {
		if st, err := os.Stat(d); filepath.IsAbs(d) && err == nil && st.IsDir() {
			have = append(have, d)
		}
	}
	return have
}
