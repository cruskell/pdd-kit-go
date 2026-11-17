package policy

// @ preserves acc(p)
// @ ensures enabled == p.Enabled
func (p *Policy) IsEnabled() (enabled bool) {
	return p.Enabled
}
