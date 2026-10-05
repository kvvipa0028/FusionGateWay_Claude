//go:build darwin

package evidence

import (
	"context"
	"encoding/json"
	"testing"
)

func TestStoredVerificationRequiresOwnedExportAndExactRestoredSource(t *testing.T) {
	a, s, _ := fixture(t, "passed")
	r, err := Run(context.Background(), a, private(t), s)
	if err != nil {
		t.Fatal(err)
	}
	v, err := Export(r, a, s)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	var recovered Stored
	if json.Unmarshal(raw, &recovered) != nil || EvaluateStored(recovered, a, s).Status != Passed {
		t.Fatal("stored controlled verification lost")
	}
	if _, err := Export(Result{}, a, s); err == nil {
		t.Fatal("JSON created owned receipt")
	}
	other, _, _ := fixture(t, "passed")
	if EvaluateStored(recovered, other, s).Status != Superseded {
		t.Fatal("foreign equal-content source reused proof")
	}
	for _, mutate := range []func(*Stored){func(v *Stored) { v.Report[0] = '!' }, func(v *Stored) { v.Record.ExitCode = 7 }, func(v *Stored) { v.Record.Environment[0] = "forged" }, func(v *Stored) { v.Record.ToolVersion = "forged" }, func(v *Stored) { v.Record.Executed = false }} {
		var bad Stored
		_ = json.Unmarshal(raw, &bad)
		mutate(&bad)
		if EvaluateStored(bad, a, s).Status == Passed {
			t.Fatal("damaged stored verification passed")
		}
	}
	s.Rules.MinTests = 2
	if EvaluateStored(recovered, a, s).Status != Superseded {
		t.Fatal("standard change retained proof")
	}
}
