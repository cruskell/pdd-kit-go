package login

import "testing"

// This checks the Go boundary's wiring on finite cases; it is empirical evidence.
func TestLoginBoundary(t *testing.T) {
	cases := []struct{ exists, matches, locked, want bool }{
		{true, true, false, true}, {true, false, false, false},
		{false, true, false, false}, {true, true, true, false},
	}
	for _, c := range cases {
		if got := DecideLogin(c.exists, c.matches, c.locked); got != c.want {
			t.Errorf("DecideLogin(%v,%v,%v)=%v, want %v", c.exists, c.matches, c.locked, got, c.want)
		}
	}
}
