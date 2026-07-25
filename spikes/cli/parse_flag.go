//go:build !cliffcli && !clihand && !clisub && !clicobra && !clikong && !cligoarg

// Variant A — stdlib `flag`, one FlagSet per subcommand + manual dispatch. Zero deps.
package main

import (
	"errors"
	"flag"
	"io"
)

func dispatch(args []string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("no command")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "greet":
		fs := flag.NewFlagSet("greet", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		name := fs.String("name", "", "name to greet")
		shout := fs.Bool("shout", false, "uppercase")
		times := fs.Int("times", 1, "repeat count")
		if err := fs.Parse(rest); err != nil {
			return "", err
		}
		n := *name
		if n == "" && fs.NArg() >= 1 {
			n = fs.Arg(0)
		}
		return formatGreet(n, *shout, *times), nil

	case "count":
		fs := flag.NewFlagSet("count", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		n := fs.Int("n", 0, "count value")
		step := fs.Int("step", 1, "step value")
		if err := fs.Parse(rest); err != nil {
			return "", err
		}
		seenN := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "n" {
				seenN = true
			}
		})
		if !seenN {
			return "", errors.New("count: missing required -n")
		}
		return formatCount(*n, *step), nil

	case "host":
		if len(rest) == 0 {
			return "", errors.New("host: missing subcommand")
		}
		switch rest[0] {
		case "add":
			if len(rest) < 2 {
				return "", errors.New("host add: missing tier")
			}
			return formatHostAdd(rest[1]), nil
		default:
			return "", errors.New("unknown host subcommand: " + rest[0])
		}

	default:
		return "", errors.New("unknown command: " + cmd)
	}
}
