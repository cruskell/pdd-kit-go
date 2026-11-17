package cli

import (
	"fmt"
	"strconv"
	"strings"
)

type globals struct {
	cwd, plan string
	json      bool
}

func parseGlobals(args []string, cwd string) (globals, []string, error) {
	options := globals{cwd: cwd, plan: "default"}
	var rest []string
	// Values of command options belong to those options, even when they happen
	// to look like global flags. A -- sentinel ends all global option extraction.
	values := map[string]bool{"--from": true, "--to": true, "--reviewer": true, "--accept-tcb": true, "--expect-digest": true, "--go": true, "--gobra-jar": true, "--timeout": true}
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
		case "--cwd", "-C", "--plan":
			if !equals {
				if i+1 >= len(args) {
					return options, rest, fmt.Errorf("%s requires a value", key)
				}
				i++
				value = args[i]
			}
			if key == "--plan" {
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
