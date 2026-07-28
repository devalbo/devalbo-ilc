// The template renderer: a mustache subset over a fixed dictionary. The rules
// worth pinning are the failures — an unknown token must never ship into a
// user's project, because it surfaces as a baffling error in a file they did not
// write, long after the template that caused it.
//
// Internal test (package engine): the renderer is an implementation detail, and
// what crosses the command boundary is the finished tree.
package engine

import (
	"strings"
	"testing"

	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"
)

func TestRenderSubstitutes(t *testing.T) {
	vars := map[string]string{"ProjectName": "myapp", "Module": "example.com/x"}
	cases := map[string]string{
		"hello {{ProjectName}}":      "hello myapp",
		"hello {{.ProjectName}}":     "hello myapp", // Go-template style
		"hello {{ .ProjectName }}":   "hello myapp", // whitespace
		"{{ProjectName}}/{{Module}}": "myapp/example.com/x",
		"no tokens here":             "no tokens here",
		"":                           "",
	}
	for in, want := range cases {
		got, err := render(in, vars)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestRenderRejectsUnknownTokens(t *testing.T) {
	vars := map[string]string{"ProjectName": "myapp"}
	for _, in := range []string{
		"{{Nope}}",
		"{{.AppNam}}", // the typo this exists to catch
		"{{ ProjectName2 }}",
		"ok {{ProjectName}} then {{Bad}}",
	} {
		if _, err := render(in, vars); err == nil {
			t.Errorf("%q: expected an error", in)
		}
	}

	// The message has to be actionable: name the bad token AND the good ones,
	// because the fix is almost always visible in the list.
	_, err := render("{{Nope}}", vars)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"Nope", "ProjectName"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestRenderRejectsUnterminated(t *testing.T) {
	if _, err := render("hello {{ProjectName", map[string]string{"ProjectName": "x"}); err == nil {
		t.Error("expected an error for an unterminated token")
	}
}

// Every token used anywhere in the shipped templates must be in the dictionary.
// Rendering the real tree is the check: an unknown token in any file — or in any
// PATH — fails here, at `go test` time, rather than when a user runs `dlc new`.
func TestShippedTemplatesRender(t *testing.T) {
	req := &dlcv1.NewRequest{
		Name:         "probe",
		Module:       "example.com/probe",
		PlatformPath: "/tmp/platform", Tiers: []string{"native", "web"}}
	files, err := scaffoldFiles(scaffoldVars(req), requestedTiers(req))
	if err != nil {
		t.Fatalf("the shipped templates do not render: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no template files rendered")
	}
	for _, f := range files {
		if strings.Contains(f.Path, "{{") || strings.Contains(f.Path, ".tmpl") {
			t.Errorf("%s: path not fully rendered", f.Path)
		}
		if strings.Contains(string(f.Content), "{{") {
			t.Errorf("%s: content still holds a token", f.Path)
		}
	}
}

// The dictionary is the contract between the COMMAND and templates/: every token
// a template may use comes from a NewRequest field or a derivation of one. Pin
// the set, so adding a request field without wiring it (or vice versa) is loud.
func TestScaffoldVarsDictionary(t *testing.T) {
	vars := scaffoldVars(&dlcv1.NewRequest{Name: "my-app", Module: "example.com/my-app", Tiers: []string{"native", "web"}})
	want := []string{
		"ProjectName", "ProjectVersion", "Module", "PkgName",
		"PlatformPath", "PlatformPathFrontend", "PlatformReplace",
		"TierSections",
	}
	if len(vars) != len(want) {
		t.Errorf("dictionary has %d keys, want %d: %v", len(vars), len(want), vars)
	}
	for _, k := range want {
		if _, ok := vars[k]; !ok {
			t.Errorf("missing token %q", k)
		}
	}
	// PkgName is derived, not copied — dashes are illegal in proto packages and
	// Go import names, so an app name that is fine on the command line is not.
	if vars["PkgName"] != "my_app" {
		t.Errorf("PkgName: got %q, want %q", vars["PkgName"], "my_app")
	}
	if vars["ProjectName"] != "my-app" {
		t.Errorf("ProjectName should be untouched: %q", vars["ProjectName"])
	}
	// The version a new project starts at. It lands in dlc.toml, and every other
	// copy is generated from there — nothing else should hard-code it.
	if vars["ProjectVersion"] != "0.1.0" {
		t.Errorf("ProjectVersion: got %q, want 0.1.0", vars["ProjectVersion"])
	}

	// A relative platform path is re-based for the tier slot's DEPTH — the web
	// slot is `hosts/web`, two levels down, so a project-root `../..` becomes
	// `../../../..`. This pins the depth rather than a single "..": the re-base
	// used to ignore its subdir argument, which was invisible while every caller
	// happened to be one level down.
	rel := scaffoldVars(&dlcv1.NewRequest{Name: "x", PlatformPath: "../..", Tiers: []string{"native", "web"}})
	if rel["PlatformPathFrontend"] != "../../../.." {
		t.Errorf("relative re-base: got %q, want ../../../..", rel["PlatformPathFrontend"])
	}
	abs := scaffoldVars(&dlcv1.NewRequest{Name: "x", PlatformPath: "/opt/ilc", Tiers: []string{"native", "web"}})
	if abs["PlatformPathFrontend"] != "/opt/ilc" {
		t.Errorf("absolute path should pass through: %q", abs["PlatformPathFrontend"])
	}
}
