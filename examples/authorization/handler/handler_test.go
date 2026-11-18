package handler

import (
	"github.com/cruskell/proofsteer-go/examples/authorization/policy"
	"testing"
)

func TestAuthorization(t *testing.T) {
	for _, c := range []struct {
		enabled      bool
		minimum, age int
		locked, want bool
	}{
		{true, 18, 18, false, true},
		{true, 18, 17, false, false},
		{true, 18, 30, true, false},
		{false, 18, 30, false, false},
	} {
		if got := Authorize(c.enabled, c.minimum, c.age, c.locked); got != c.want {
			t.Errorf("Authorize(%+v)=%v, want %v", c, got, c.want)
		}
	}
}

func TestPolicyPointer(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		p := policy.Policy{Enabled: enabled}
		if got := p.IsEnabled(); got != enabled {
			t.Errorf("IsEnabled()=%v, want %v", got, enabled)
		}
	}
}
