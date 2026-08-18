package clispec_test

import (
	"strings"
	"testing"

	"github.com/devalbo/devalbo-ilc/dlc-platform/clispec"
)

func TestShortEnum(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"PROBLEM_UNSPECIFIED,PROBLEM_DIVIDE_BY_ZERO,PROBLEM_OVERFLOW", "unspecified,divide_by_zero,overflow"},
		{"STYLE_UNSPECIFIED,STYLE_PLAIN,STYLE_ROCKET", "unspecified,plain,rocket"},
		{"SPEC_KIND_UNSPECIFIED,SPEC_KIND_STRING,SPEC_KIND_ENUM", "unspecified,string,enum"},
		{"ONLY_ONE", "ONLY_ONE"},
		{"RED,GREEN", "RED,GREEN"},
	} {
		got := strings.Join(clispec.ShortEnum(strings.Split(c.in, ",")), ",")
		if got != c.want {
			t.Errorf("%s -> %s, want %s", c.in, got, c.want)
		}
	}
}
