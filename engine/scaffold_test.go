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
	vars := map[string]string{"AppName": "myapp", "Module": "example.com/x"}
	cases := map[string]string{
		"hello {{AppName}}":      "hello myapp",
		"hello {{.AppName}}":     "hello myapp", // Go-template style
		"hello {{ .AppName }}":   "hello myapp", // whitespace
		"{{AppName}}/{{Module}}": "myapp/example.com/x",
		"no tokens here":         "no tokens here",
		"":                       "",
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
	vars := map[string]string{"AppName": "myapp"}
	for _, in := range []string{
		"{{Nope}}",
		"{{.AppNam}}", // the typo this exists to catch
		"{{ AppName2 }}",
		"ok {{AppName}} then {{Bad}}",
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
	for _, want := range []string{"Nope", "AppName"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestRenderRejectsUnterminated(t *testing.T) {
	if _, err := render("hello {{AppName", map[string]string{"AppName": "x"}); err == nil {
		t.Error("expected an error for an unterminated token")
	}
}

// Every token used anywhere in the shipped templates must be in the dictionary.
// Rendering the real tree is the check: an unknown token in any file — or in any
// PATH — fails here, at `go test` time, rather than when a user runs `dlc new`.
func TestShippedTemplatesRender(t *testing.T) {
	files, err := scaffoldFiles(scaffoldVars(&dlcv1.NewRequest{
		Name:         "probe",
		Module:       "example.com/probe",
		PlatformPath: "/tmp/platform",
	}))
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
	vars := scaffoldVars(&dlcv1.NewRequest{Name: "my-app", Module: "example.com/my-app"})
	want := []string{"AppName", "Module", "PkgName", "PlatformPath", "PlatformReplace"}
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
	if vars["AppName"] != "my-app" {
		t.Errorf("AppName should be untouched: %q", vars["AppName"])
	}
}
