package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type globals struct {
	cwd, plan, policy string
	json              bool
}

func parseGlobals(args []string, cwd string) (globals, []string, error) {
	options := globals{cwd: cwd, plan: "default", policy: os.Getenv("PDD_REVIEW_POLICY")}
	var rest []string
	// Values of command options belong to those options, even when they happen
	// to look like global flags. A -- sentinel ends all global option extraction.
	values := map[string]bool{"--request-file": true, "--driver": true, "--worker": true, "--files": true, "--agent-timeout": true, "--expect-draft": true, "--id": true, "--option": true, "--reason": true, "--role": true, "--max-iterations": true, "--out": true, "--title": true, "--base": true, "--head": true, "--from": true, "--to": true, "--reviewer": true, "--accept-tcb": true, "--expect-digest": true, "--go": true, "--gobra-jar": true, "--timeout": true, "--signing-key": true, "--private-key": true, "--public-key": true}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, value, equals := strings.Cut(arg, "=")
		switch key {
		case "--":
			rest = append(rest, args[i:]...)
			return options, rest, nil
		case "--json":
			options.json = true
			if equals {
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return options, rest, fmt.Errorf("--json expects true or false")
				}
				options.json = parsed
			}
		case "--cwd", "-C", "--plan", "--policy":
			if !equals {
				if i+1 >= len(args) {
					return options, rest, fmt.Errorf("%s requires a value", key)
				}
				i++
				value = args[i]
			}
			if key == "--policy" {
				options.policy = value
			} else if key == "--plan" {
				options.plan = value
			} else {
				options.cwd = value
			}
		default:
			rest = append(rest, arg)
			if values[key] && !equals && i+1 < len(args) {
				i++
				rest = append(rest, args[i])
			}
		}
	}
	return options, rest, nil
}
