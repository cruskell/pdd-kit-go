package login

// DecideLogin is the production login decision. Lookup and password-check
// results are inputs: database and cryptographic correctness remain trusted.
// @ ensures allowed == (accountExists && passwordMatches && !locked)
func DecideLogin(accountExists, passwordMatches, locked bool) (allowed bool) {
	return accountExists && passwordMatches && !locked
}
