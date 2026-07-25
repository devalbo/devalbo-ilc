//go:build cliffcli

// Variant B — peterbourgon/ff/v3/ffcli: explicit subcommand tree over stdlib flag.
package main

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/peterbourgon/ff/v3/ffcli"
)

func dispatch(args []string) (string, error) {
	var result string

	greetFS := flag.NewFlagSet("greet", flag.ContinueOnError)
	greetFS.SetOutput(io.Discard)
	greetName := greetFS.String("name", "", "name to greet")
	greetShout := greetFS.Bool("shout", false, "uppercase")
	greetTimes := greetFS.Int("times", 1, "repeat count")
	greet := &ffcli.Command{
		Name:       "greet",
		FlagSet:    greetFS,
		ShortUsage: "greet [flags] [name]",
		Exec: func(_ context.Context, args []string) error {
			n := *greetName
			if n == "" && len(args) >= 1 {
				n = args[0]
			}
			result = formatGreet(n, *greetShout, *greetTimes)
			return nil
		},
	}

	countFS := flag.NewFlagSet("count", flag.ContinueOnError)
	countFS.SetOutput(io.Discard)
	countN := countFS.Int("n", 0, "count value")
	countStep := countFS.Int("step", 1, "step value")
	count := &ffcli.Command{
		Name:    "count",
		FlagSet: countFS,
		Exec: func(context.Context, []string) error {
			seenN := false
			countFS.Visit(func(f *flag.Flag) {
				if f.Name == "n" {
					seenN = true
				}
			})
			if !seenN {
				return errors.New("count: missing required -n")
			}
			result = formatCount(*countN, *countStep)
			return nil
		},
	}

	hostAdd := &ffcli.Command{
		Name:       "add",
		ShortUsage: "host add <tier>",
		Exec: func(_ context.Context, args []string) error {
			if len(args) < 1 {
				return errors.New("host add: missing tier")
			}
			result = formatHostAdd(args[0])
			return nil
		},
	}
	host := &ffcli.Command{
		Name:        "host",
		Subcommands: []*ffcli.Command{hostAdd},
		Exec: func(context.Context, []string) error {
			return errors.New("host: missing subcommand")
		},
	}

	root := &ffcli.Command{
		Subcommands: []*ffcli.Command{greet, count, host},
		Exec: func(context.Context, []string) error {
			return errors.New("no command")
		},
	}
	if err := root.ParseAndRun(context.Background(), args); err != nil {
		return "", err
	}
	return result, nil
}
