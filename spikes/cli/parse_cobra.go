//go:build clicobra

// Variant — spf13/cobra (+ pflag). Help uses text/template (reflection); SilenceUsage
// avoids that path for the matrix.
package main

import (
	"errors"
	"strconv"

	"github.com/spf13/cobra"
)

func dispatch(args []string) (string, error) {
	var result string

	root := &cobra.Command{
		Use:           "cli",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(*cobra.Command, []string) error {
			return errors.New("no command")
		},
	}

	var greetName string
	var greetShout bool
	var greetTimes int
	greet := &cobra.Command{
		Use:  "greet [name]",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, pos []string) error {
			n := greetName
			if n == "" && len(pos) >= 1 {
				n = pos[0]
			}
			result = formatGreet(n, greetShout, greetTimes)
			return nil
		},
	}
	greet.Flags().StringVar(&greetName, "name", "", "name to greet")
	greet.Flags().BoolVar(&greetShout, "shout", false, "uppercase")
	greet.Flags().IntVar(&greetTimes, "times", 1, "repeat count")

	var countN int
	var countStep int
	var countNSet bool
	count := &cobra.Command{
		Use: "count",
		RunE: func(*cobra.Command, []string) error {
			if !countNSet {
				return errors.New("count: missing required -n")
			}
			result = formatCount(countN, countStep)
			return nil
		},
	}
	count.Flags().IntVarP(&countN, "n", "n", 0, "count value")
	count.Flags().IntVar(&countStep, "step", 1, "step value")
	count.PreRunE = func(cmd *cobra.Command, _ []string) error {
		countNSet = cmd.Flags().Changed("n")
		return nil
	}

	host := &cobra.Command{
		Use: "host",
		RunE: func(*cobra.Command, []string) error {
			return errors.New("host: missing subcommand")
		},
	}
	hostAdd := &cobra.Command{
		Use:  "add [tier]",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, pos []string) error {
			result = formatHostAdd(pos[0])
			return nil
		},
	}
	host.AddCommand(hostAdd)

	root.AddCommand(greet, count, host)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		// Normalize type errors from pflag into a non-success for the harness.
		if _, ok := err.(*strconv.NumError); ok {
			return "", err
		}
		return "", err
	}
	return result, nil
}
