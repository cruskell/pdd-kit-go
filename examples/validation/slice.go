// Package validation contains a permission-aware slice validation core.
package validation

// @ requires len(values) <= 9223372036854775807
// @ preserves forall k int :: 0 <= k && k < len(values) ==> acc(&values[k], 1/2)
// @ ensures valid == (forall k int :: 0 <= k && k < len(values) ==> values[k] >= 0)
func AllNonnegative(values []int) (valid bool) {
	// @ invariant 0 <= i && i <= len(values)
	// @ invariant forall k int :: 0 <= k && k < len(values) ==> acc(&values[k], 1/2)
	// @ invariant forall k int :: 0 <= k && k < i ==> values[k] >= 0
	for i := 0; i < len(values); i++ {
		if values[i] < 0 {
			return false
		}
	}
	return true
}
