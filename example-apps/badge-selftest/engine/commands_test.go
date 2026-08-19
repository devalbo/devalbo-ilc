package engine

import (
	"strings"
	"testing"

	badgeselftestv1 "github.com/you/badge-selftest/gen/go/badge_selftest/v1"
)

// TestSelfTestReportsRatherThanFails pins the behaviour that makes this app
// usable: a self-test whose checks fail has still SUCCEEDED at its job. The
// finding is the payload, and returning an error would make every host show
// "the app failed" instead of the report.
func TestSelfTestReportsRatherThanFails(t *testing.T) {
	got, err := handleSelfTest(&badgeselftestv1.SelfTestRequest{})
	if err != nil {
		t.Fatalf("a self-test must not return an error: %v", err)
	}
	if got.Checks == 0 {
		t.Fatal("no checks ran")
	}
	if got.Passed > got.Checks {
		t.Fatalf("passed %d of %d", got.Passed, got.Checks)
	}
	if !strings.Contains(got.Text, "passed") {
		t.Errorf("report should carry a tally, got %q", got.Text)
	}
}

// TestAbsentWorldIsNotAFailure — the `undefined` world declares nothing, which
// is legitimate and common. An earlier version reported FAIL here, and a check
// that fails on correct behaviour trains people to ignore it.
func TestAbsentWorldIsNotAFailure(t *testing.T) {
	// No manifest sent: every field is its zero value, which is what a host that
	// says nothing about itself looks like.
	for _, c := range checkManifest() {
		if !c.passed {
			t.Errorf("an unadorned host must not fail: %s = %s", c.name, c.detail)
		}
	}
}

// TestWorldCannotBeHalfDeclared — the failure mode this refactor DELETED.
//
// The old check hunted for a host that set `ILC_TIER` and forgot `ILC_WORLD`,
// because with N independent string keys that is a real bug an app could detect.
// A typed manifest has no such state: `Identity` is one message with one field,
// so "half declared" is not expressible and there is nothing left to check.
//
// Kept as a test rather than deleted so the absence is deliberate. If somebody
// adds a second identity field, this is where they should ask whether they have
// re-created the problem.
func TestWorldCannotBeHalfDeclared(t *testing.T) {
	for _, c := range checkManifest() {
		if c.name == "world" && strings.Contains(c.detail, "partial") {
			t.Errorf("a typed manifest should have no partial state: %s", c.detail)
		}
	}
}
