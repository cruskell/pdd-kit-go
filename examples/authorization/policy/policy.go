package policy

// @ ensures allowed == (p.Enabled && age >= p.MinimumAge && !locked)
func (p Policy) Allows(age int, locked bool) (allowed bool) {
	return p.Enabled && age >= p.MinimumAge && !locked
}
