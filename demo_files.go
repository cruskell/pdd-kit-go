package pddkit

import (
	"embed"
	"io/fs"
)

// Explicit file patterns keep the bundled demo identical to the reviewed
// repository example and exclude workspace state from installed binaries.
//
//go:embed go.mod .pdd/plan.json .pdd/reviews/default.json examples/login/login.go examples/login/login_test.go
var demoFiles embed.FS

// DemoFiles returns the login example and its portable demonstration approval.
func DemoFiles() fs.FS { return demoFiles }
