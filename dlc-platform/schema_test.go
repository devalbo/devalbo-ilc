package platform

import "testing"

// ONE CONVENTION, TWO IMPLEMENTATIONS — Go here and TypeScript in
// dlc-platform/web/schema.ts. That is the arrangement this project keeps finding
// bugs in, so the cases are pinned here and mirrored there rather than left to
// agree by inspection.
func TestSchemaURL(t *testing.T) {
	cases := []struct {
		name string
		host SchemaHost
		id   string
		want string
	}{
		{
			name: "the ordinary case",
			host: SchemaHost{Base: "https://schemas.example.com/dlc"},
			id:   "abc123",
			want: "https://schemas.example.com/dlc/abc123.binpb",
		},
		{
			// A base is something a person types into a config file, and "it
			// worked until I added a slash" is a bad way to spend an afternoon.
			name: "a trailing slash is tolerated",
			host: SchemaHost{Base: "https://schemas.example.com/dlc/"},
			id:   "abc123",
			want: "https://schemas.example.com/dlc/abc123.binpb",
		},
		{
			name: "a relative base, for a page serving its own schemas",
			host: SchemaHost{Base: "/schemas"},
			id:   "abc123",
			want: "/schemas/abc123.binpb",
		},
		{
			name: "a custom extension",
			host: SchemaHost{Base: "/schemas", Extension: ".pb"},
			id:   "abc123",
			want: "/schemas/abc123.pb",
		},
		{
			// A hash should never contain one of these, but an id is a string an
			// app chose and escaping is cheaper than trusting.
			name: "an id needing escaping",
			host: SchemaHost{Base: "/schemas"},
			id:   "a/b c",
			want: "/schemas/a%2Fb%20c.binpb",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SchemaURL(c.host, c.id); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// An app that declared nothing must still ANSWER, so a client learns it cannot
// describe itself in one round trip rather than by timing out.
func TestGetSchemaAnswersWhenNothingWasDeclared(t *testing.T) {
	appSchema = nil
	res, err := handleGetSchema(nil)
	if err != nil {
		t.Fatalf("the verb failed: %v", err)
	}
	if res.GetSchema() != nil {
		t.Fatalf("expected an absent schema, got %v", res.GetSchema())
	}
}

func TestSetHostedSchemaDerivesTheURL(t *testing.T) {
	t.Cleanup(func() { appSchema = nil })
	SetHostedSchema(SchemaHost{Base: "/schemas"}, SchemaDecl{ID: "deadbeef", Version: "1.4.0"})
	got := Schema()
	if got.GetUrl() != "/schemas/deadbeef.binpb" {
		t.Fatalf("url = %q", got.GetUrl())
	}
	if got.GetSchemaId() != "deadbeef" || got.GetVersion() != "1.4.0" {
		t.Fatalf("id/version = %q/%q", got.GetSchemaId(), got.GetVersion())
	}
}
