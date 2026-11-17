package pdd

// Windows access control is managed by the operator's ACL. os.FileMode does not
// expose a meaningful ACL; this function deliberately makes no ACL assertion.
func checkPrivateKeyPermissions(string) error { return nil }
