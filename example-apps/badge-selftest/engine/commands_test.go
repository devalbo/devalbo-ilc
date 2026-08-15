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

// TestAbsentAdvertisementIsNotAFailure — the `undefined` world advertises
// nothing, which is legitimate and common. An earlier version reported FAIL
// here, and a check that fails on correct behaviour trains people to ignore it.
func TestAbsentAdvertisementIsNotAFailure(t *testing.T) {
	t.Setenv("ILC_TIER", "")
	t.Setenv("ILC_WORLD", "")
	for _, c := range checkAdvertisement() {
		if c.name == "advertisement" && !c.passed {
			t.Errorf("an unadorned host must not fail: %s", c.detail)
		}
	}
}

// TestPartialAdvertisementIsAFailure — the case that IS a host bug: keys set
// halfway, which means somebody added one and forgot the table it belongs to.
func TestPartialAdvertisementIsAFailure(t *testing.T) {
	t.Setenv("ILC_TIER", "rp2350")
	t.Setenv("ILC_WORLD", "")
	found := false
	for _, c := range checkAdvertisement() {
		if c.name == "advertisement" {
			found = true
			if c.passed {
				t.Error("a half-set advertisement should be caught")
			}
		}
	}
	if !found {
		t.Fatal("no advertisement check ran")
	}
}

// TestReadOnlySkipsWrites — a host may be mounted read-only elsewhere (the
// badge's USB volume is single-writer), and a self-test that corrupts what it is
// testing is worse than no self-test.
func TestReadOnlySkipsWrites(t *testing.T) {
	got, err := handleSelfTest(&badgeselftestv1.SelfTestRequest{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Text, "read-only") {
		t.Errorf("read-only run should say so: %q", got.Text)
	}
}
