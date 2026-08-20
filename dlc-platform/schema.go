package platform

import (
	"net/url"
	"strings"

	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// WHAT THIS APP'S MESSAGES LOOK LIKE, for a client that was never built against
// them.
//
// # Why an app has to opt in
//
// `GetCommandSpec` already describes REQUEST fields, and that is enough to build
// a request for an app a host has never seen — a badge collected
// `-set opponent=<enum>` from tictactoe's own proto without knowing what
// tic-tac-toe is. Responses stop short: `SpecResult` is flat, so a reply whose
// payload is a nested message arrives as bytes with no names.
//
// The schema closes that, and it costs something — a descriptor is 5-15 KB in
// the artifact — so it is DECLARED rather than automatic. An app that never
// meets a generic client pays nothing; one that will can choose how much to
// carry: an id and a URL for a microcontroller counting bytes, the whole
// descriptor for a badge that may be offline.
//
// # Absence is a no-op, not an error
//
// The verb always answers. An app that declared nothing returns an empty
// `SchemaInfo`, which tells a client to fall back to `SpecResult` — as opposed
// to a verb that is missing, which a client can only discover by timing out.
var appSchema *ilcv1.SchemaInfo

// SetSchema declares this app's schema. Call it from the app's init.
//
// EVERY FIELD IS OPTIONAL EXCEPT THE FIRST, and the first is what makes the rest
// worth anything: `schemaID` is a content hash of the descriptor, so a client
// that fetched `url` can check that what it got matches what these bytes were
// built from. Without it a URL is a promise nobody can verify, and a schema
// fetched from a drifted URL renders wrong names over correct bytes — silently,
// which is the failure this whole mechanism exists to avoid.
func SetSchema(schemaID, version, at string, descriptor []byte) {
	// `at` rather than `url`: the parameter would shadow the imported `net/url`,
	// which compiles today only because this function does not use the package.
	appSchema = &ilcv1.SchemaInfo{
		SchemaId:    schemaID,
		Version:     version,
		Url:         at,
		Descriptor_: descriptor,
	}
}

// SchemaHost is where an app's schemas are published: a base route, and the
// extension the descriptors are served under.
//
// A CONVENTION RATHER THAN A URL PER APP, because the alternative is every app
// inventing a layout and every reader hard-coding the one it happens to know. A
// fleet produces many apps and one schema host; a base plus a rule serves all of
// them.
type SchemaHost struct {
	// e.g. "https://schemas.example.com/dlc" or "/schemas".
	Base string
	// Defaults to ".binpb" when empty.
	Extension string
}

// SchemaURL is where `schemaID` lives under `host`.
//
// THE ID IS IN THE PATH, which makes the URL content-addressed and therefore
// immutable. A path naming only the app resolves to whatever is there now, and
// when that drifts from the firmware a reader renders wrong names over correct
// bytes with nothing to say so. With the id in the path a hit is the right
// schema by construction, and the response is cacheable forever.
//
// MIRRORS `schemaUrl` in dlc-platform/web/schema.ts. Two implementations of one
// convention is exactly the arrangement this project keeps finding bugs in, so
// `TestSchemaURL` pins them to the same cases — a rule this
// short is cheaper to test than to generate.
func SchemaURL(host SchemaHost, schemaID string) string {
	base := strings.TrimRight(host.Base, "/")
	ext := host.Extension
	if ext == "" {
		ext = ".binpb"
	}
	return base + "/" + url.PathEscape(schemaID) + ext
}

// SetHostedSchema declares a schema published under `host`, deriving the URL.
//
// The common case: an app knows its base route from config and its id from its
// build, and should not have to concatenate them itself — that is where a
// trailing slash or a missing extension becomes an afternoon.
func SetHostedSchema(host SchemaHost, schemaID, version string, descriptor []byte) {
	SetSchema(schemaID, version, SchemaURL(host, schemaID), descriptor)
}

// Schema returns what this app declared, or nil.
func Schema() *ilcv1.SchemaInfo {
	return appSchema
}

func handleGetSchema(_ *ilcv1.GetSchemaRequest) (*ilcv1.GetSchemaResponse, error) {
	// NIL IS A LEGAL ANSWER and travels as an absent field. A client reading it
	// learns "this app does not describe itself" in one round trip.
	return &ilcv1.GetSchemaResponse{Schema: appSchema}, nil
}
