// Shared output formatting for every parser variant (identical matrix expects).
package main

import (
	"strconv"
	"strings"
)

func formatGreet(name string, shout bool, times int) string {
	if times < 1 {
		times = 1
	}
	msg := "hello " + name
	if shout {
		msg = strings.ToUpper(msg)
	}
	if times == 1 {
		return msg
	}
	parts := make([]string, times)
	for i := 0; i < times; i++ {
		parts[i] = msg
	}
	return strings.Join(parts, " ")
}

func formatCount(n, step int) string {
	return "count=" + strconv.Itoa(n) + " step=" + strconv.Itoa(step)
}

func formatHostAdd(tier string) string {
	return "host+" + tier
}
