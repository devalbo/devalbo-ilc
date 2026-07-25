//go:build clihand

// Variant C — hand-rolled argv parser (guaranteed TinyGo fallback).
package main

import (
	"errors"
	"strconv"
	"strings"
)

func dispatch(args []string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("no command")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "greet":
		return parseGreetHand(rest)
	case "count":
		return parseCountHand(rest)
	case "host":
		return parseHostHand(rest)
	default:
		return "", errors.New("unknown command: " + cmd)
	}
}

func parseGreetHand(args []string) (string, error) {
	var name string
	shout := false
	times := 1
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--shout":
			shout = true
		case a == "--name" || a == "-name":
			if i+1 >= len(args) {
				return "", errors.New("greet: --name needs a value")
			}
			i++
			name = args[i]
		case strings.HasPrefix(a, "--name="):
			name = strings.TrimPrefix(a, "--name=")
		case strings.HasPrefix(a, "-name="):
			name = strings.TrimPrefix(a, "-name=")
		case a == "--times" || a == "-times":
			if i+1 >= len(args) {
				return "", errors.New("greet: --times needs a value")
			}
			i++
			v, err := strconv.Atoi(args[i])
			if err != nil {
				return "", err
			}
			times = v
		case strings.HasPrefix(a, "--times="):
			v, err := strconv.Atoi(strings.TrimPrefix(a, "--times="))
			if err != nil {
				return "", err
			}
			times = v
		case strings.HasPrefix(a, "-") && a != "-":
			return "", errors.New("unknown flag: " + a)
		default:
			positional = append(positional, a)
		}
	}
	if name == "" && len(positional) >= 1 {
		name = positional[0]
	}
	return formatGreet(name, shout, times), nil
}

func parseCountHand(args []string) (string, error) {
	seenN := false
	n := 0
	step := 1
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-n" || a == "--n":
			if i+1 >= len(args) {
				return "", errors.New("count: -n needs a value")
			}
			i++
			v, err := strconv.Atoi(args[i])
			if err != nil {
				return "", err
			}
			n = v
			seenN = true
		case strings.HasPrefix(a, "-n="):
			v, err := strconv.Atoi(strings.TrimPrefix(a, "-n="))
			if err != nil {
				return "", err
			}
			n = v
			seenN = true
		case strings.HasPrefix(a, "--n="):
			v, err := strconv.Atoi(strings.TrimPrefix(a, "--n="))
			if err != nil {
				return "", err
			}
			n = v
			seenN = true
		case a == "--step" || a == "-step":
			if i+1 >= len(args) {
				return "", errors.New("count: --step needs a value")
			}
			i++
			v, err := strconv.Atoi(args[i])
			if err != nil {
				return "", err
			}
			step = v
		case strings.HasPrefix(a, "--step="):
			v, err := strconv.Atoi(strings.TrimPrefix(a, "--step="))
			if err != nil {
				return "", err
			}
			step = v
		case strings.HasPrefix(a, "-"):
			return "", errors.New("unknown flag: " + a)
		default:
			return "", errors.New("count: unexpected arg " + a)
		}
	}
	if !seenN {
		return "", errors.New("count: missing required -n")
	}
	return formatCount(n, step), nil
}

func parseHostHand(args []string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("host: missing subcommand")
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return "", errors.New("host add: missing tier")
		}
		return formatHostAdd(args[1]), nil
	default:
		return "", errors.New("unknown host subcommand: " + args[0])
	}
}
