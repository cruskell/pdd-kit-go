//go:build !windows

package pdd

import (
	"fmt"
	"os"
)

func checkPrivateKeyPermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("private signing key must not be accessible to group/other users; chmod 600 %s", path)
	}
	return nil
}
