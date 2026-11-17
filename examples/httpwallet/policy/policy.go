// Package policy contains the production decision shared by the HTTP service and Gobra.
package policy

// @ ensures allowed == (authenticated && !locked && amount > 0 && amount <= balance)
func CanWithdraw(authenticated, locked bool, amount, balance int) (allowed bool) {
	return authenticated && !locked && amount > 0 && amount <= balance
}
