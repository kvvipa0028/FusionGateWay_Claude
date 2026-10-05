package control

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestControllerRejectsReadonlyOrMalformedWriteScopesBeforePreparation(t *testing.T) {
	for _, paths := range [][]string{{"tests"}, {"../escape"}, {"tests", "tests"}} {
		f := newControlFixture(t)
		before, err := f.st.Task(f.in.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		f.launch.Spec.WritePaths = paths
		c := f.controller(t)
		if _, err := c.Start(context.Background(), "invalid-scope", f.in); !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid scope accepted", err)
		}
		after, err := f.st.Task(f.in.TaskID)
		if err != nil || !reflect.DeepEqual(after, before) || f.started.Load() != 0 {
			t.Fatal("invalid scope prepared/launched", err)
		}
	}
}
