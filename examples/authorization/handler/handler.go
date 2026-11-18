package handler

import "github.com/cruskell/proofsteer-go/examples/authorization/policy"

// @ ensures result == (enabled && age >= minimum && !locked)
func Authorize(enabled bool, minimum, age int, locked bool) (result bool) {
	p := policy.Policy{Enabled: enabled, MinimumAge: minimum}
	return p.Allows(age, locked)
}
