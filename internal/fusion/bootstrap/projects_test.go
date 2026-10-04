//go:build darwin || linux

package bootstrap

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/api"
)

func TestControlHostProjectIndexUsesTrustedSourceAndRevocation(t *testing.T) {
	path, d := sourceFixture(t)
	root := filepath.Join(filepath.Dir(path), "control")
	h, err := OpenControl(path, root, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveExecutionHost(t, h)
	read := func() api.ProjectsReply {
		t.Helper()
		code, b, _ := hostHTTP(t, h, "GET", "/control/v1/projects", "", "", "")
		var list api.ProjectsReply
		if code != 200 || json.Unmarshal(b, &list) != nil || len(list.Projects) != 1 || list.Projects[0].ID != "fixture-project" {
			t.Fatal("loopback project inventory", code)
		}
		if strings.Contains(string(b), root) || strings.Contains(string(b), d.Projects[0].Path) || strings.Contains(string(b), "credential_identity") {
			t.Fatal("private inventory metadata")
		}
		return list
	}
	before := read()
	code, _, _ := hostHTTP(t, h, "PUT", "/control/v1/projects/fixture-project/defaults", `{"layer":{}}`, "", `"0"`)
	if code != 201 {
		t.Fatal("default save", code)
	}
	if read().Projects[0].ConfigurationRevision != before.Projects[0].ConfigurationRevision+1 {
		t.Fatal("current project revision")
	}
	code, _, _ = hostHTTP(t, h, "POST", "/control/v1/projects", `{}`, "", "")
	if code != 405 {
		t.Fatal("project registration exposed over HTTP", code)
	}
	d.Revision++
	writeSource(t, path, d)
	code, b, _ := hostHTTP(t, h, "GET", "/control/v1/projects", "", "", "")
	if code != 503 || strings.Contains(string(b), "fixture-project") {
		t.Fatal("revoked source listed", code)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
}
