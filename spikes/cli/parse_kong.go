//go:build clikong

// Variant — alecthomas/kong (struct-tag reflection). The real TinyGo reflect test.
package main

import (
	"errors"

	"github.com/alecthomas/kong"
)

type greetKong struct {
	Name  string `arg:"" optional:"" help:"positional name"`
	NameF string `name:"name" help:"name to greet"`
	Shout bool   `name:"shout" help:"uppercase"`
	Times int    `name:"times" default:"1" help:"repeat"`
}

type countKong struct {
	N    int `name:"n" short:"n" required:"" help:"count"`
	Step int `name:"step" default:"1" help:"step"`
}

type hostAddKong struct {
	Tier string `arg:"" help:"tier"`
}

type hostKong struct {
	Add hostAddKong `cmd:"" help:"add a host tier"`
}

type cliKong struct {
	Greet greetKong `cmd:"" help:"greet"`
	Count countKong `cmd:"" help:"count"`
	Host  hostKong  `cmd:"" help:"host"`
}

func dispatch(args []string) (string, error) {
	var cli cliKong
	parser, err := kong.New(&cli,
		kong.Name("cli"),
		kong.Exit(func(int) {}),
		kong.Writers(discardWriter{}, discardWriter{}),
	)
	if err != nil {
		return "", err
	}
	ctx, err := parser.Parse(args)
	if err != nil {
		return "", err
	}

	switch ctx.Command() {
	case "greet":
		n := cli.Greet.NameF
		if n == "" {
			n = cli.Greet.Name
		}
		return formatGreet(n, cli.Greet.Shout, cli.Greet.Times), nil
	case "count":
		return formatCount(cli.Count.N, cli.Count.Step), nil
	case "host add":
		return formatHostAdd(cli.Host.Add.Tier), nil
	case "":
		return "", errors.New("no command")
	default:
		return "", errors.New("unhandled: " + ctx.Command())
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
