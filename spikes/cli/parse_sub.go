//go:build clisub

// Variant — google/subcommands (interface-based over flag). Injects os.Args because
// Commander.Execute hardcodes os.Args.
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"strconv"

	"github.com/google/subcommands"
)

func dispatch(args []string) (string, error) {
	var result string

	top := flag.NewFlagSet("cli", flag.ContinueOnError)
	top.SetOutput(io.Discard)
	cdr := subcommands.NewCommander(top, "cli")
	cdr.Output = io.Discard
	cdr.Error = io.Discard

	g := &greetCmd{out: &result}
	c := &countCmd{out: &result}
	h := &hostCmd{out: &result}
	cdr.Register(g, "")
	cdr.Register(c, "")
	cdr.Register(h, "")

	// Commander.Execute hardcodes os.Args — inject argv. Under TinyGo wasip2 this
	// assignment does not feed the commander (measured: every call → usage / fail).
	prev := os.Args
	os.Args = append([]string{"cli"}, args...)
	status := cdr.Execute(context.Background())
	os.Args = prev

	if status != subcommands.ExitSuccess {
		if len(args) == 0 {
			return "", errors.New("no command")
		}
		return "", errors.New("command failed")
	}
	return result, nil
}

type greetCmd struct {
	out   *string
	name  string
	shout bool
	times int
}

func (c *greetCmd) Name() string     { return "greet" }
func (c *greetCmd) Synopsis() string { return "greet" }
func (c *greetCmd) Usage() string    { return "greet [flags] [name]\n" }
func (c *greetCmd) SetFlags(fs *flag.FlagSet) {
	fs.SetOutput(io.Discard)
	fs.StringVar(&c.name, "name", "", "name to greet")
	fs.BoolVar(&c.shout, "shout", false, "uppercase")
	fs.IntVar(&c.times, "times", 1, "repeat count")
}
func (c *greetCmd) Execute(_ context.Context, fs *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	n := c.name
	if n == "" && fs.NArg() >= 1 {
		n = fs.Arg(0)
	}
	*c.out = formatGreet(n, c.shout, c.times)
	return subcommands.ExitSuccess
}

type countCmd struct {
	out  *string
	n    int
	step int
	seen bool
}

func (c *countCmd) Name() string     { return "count" }
func (c *countCmd) Synopsis() string { return "count" }
func (c *countCmd) Usage() string    { return "count -n N [--step N]\n" }
func (c *countCmd) SetFlags(fs *flag.FlagSet) {
	fs.SetOutput(io.Discard)
	fs.Func("n", "count value", func(s string) error {
		v, err := strconv.Atoi(s)
		if err != nil {
			return err
		}
		c.n = v
		c.seen = true
		return nil
	})
	fs.IntVar(&c.step, "step", 1, "step value")
}
func (c *countCmd) Execute(context.Context, *flag.FlagSet, ...interface{}) subcommands.ExitStatus {
	if !c.seen {
		return subcommands.ExitUsageError
	}
	*c.out = formatCount(c.n, c.step)
	return subcommands.ExitSuccess
}

type hostCmd struct{ out *string }

func (c *hostCmd) Name() string     { return "host" }
func (c *hostCmd) Synopsis() string { return "host" }
func (c *hostCmd) Usage() string    { return "host add <tier>\n" }
func (c *hostCmd) SetFlags(fs *flag.FlagSet) {
	fs.SetOutput(io.Discard)
}
func (c *hostCmd) Execute(_ context.Context, fs *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	args := fs.Args()
	if len(args) == 0 {
		return subcommands.ExitUsageError
	}
	if args[0] != "add" {
		return subcommands.ExitUsageError
	}
	if len(args) < 2 {
		return subcommands.ExitUsageError
	}
	*c.out = formatHostAdd(args[1])
	return subcommands.ExitSuccess
}
