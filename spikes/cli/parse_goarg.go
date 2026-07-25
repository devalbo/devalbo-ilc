//go:build cligoarg

// Variant — alexflint/go-arg (struct-tag reflection).
package main

import (
	"errors"

	"github.com/alexflint/go-arg"
)

type greetArg struct {
	Name  string `arg:"positional"`
	NameF string `arg:"--name"`
	Shout bool   `arg:"--shout"`
	Times int    `arg:"--times" default:"1"`
}

type countArg struct {
	N    *int `arg:"-n,--n,required"`
	Step int  `arg:"--step" default:"1"`
}

type hostAddArg struct {
	Tier string `arg:"positional,required"`
}

type hostArg struct {
	Add *hostAddArg `arg:"subcommand:add"`
}

type cliArg struct {
	Greet *greetArg `arg:"subcommand:greet"`
	Count *countArg `arg:"subcommand:count"`
	Host  *hostArg  `arg:"subcommand:host"`
}

func dispatch(args []string) (string, error) {
	var cli cliArg
	p, err := arg.NewParser(arg.Config{
		Program:   "cli",
		IgnoreEnv: true,
		Exit:      func(int) {},
		Out:       discardArgWriter{},
	}, &cli)
	if err != nil {
		return "", err
	}
	if err := p.Parse(args); err != nil {
		return "", err
	}

	switch {
	case cli.Greet != nil:
		n := cli.Greet.NameF
		if n == "" {
			n = cli.Greet.Name
		}
		return formatGreet(n, cli.Greet.Shout, cli.Greet.Times), nil
	case cli.Count != nil:
		if cli.Count.N == nil {
			return "", errors.New("count: missing required -n")
		}
		return formatCount(*cli.Count.N, cli.Count.Step), nil
	case cli.Host != nil:
		if cli.Host.Add == nil {
			return "", errors.New("host: missing subcommand")
		}
		return formatHostAdd(cli.Host.Add.Tier), nil
	default:
		return "", errors.New("no command")
	}
}

type discardArgWriter struct{}

func (discardArgWriter) Write(p []byte) (int, error) { return len(p), nil }
