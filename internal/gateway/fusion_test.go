//go:build fusion

package gateway

import "testing"

func TestFusionDefaultPortIsIndependent(t *testing.T) {
	t.Setenv("MAGPIE_ADDR", "")
	if got := Addr(); got != "127.0.0.1:3426" {
		t.Fatalf("address = %q, want independent loopback", got)
	}
}
