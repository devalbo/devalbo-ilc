// The platform's half of the derived index (docs/INDEX-PLAN.md Phase 2): the
// `rebuild-index` verb, when it is registered, and the rule that the index never
// leaves in a bundle. The index's own behaviour lives in index/.
package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// --- registration (D3/D8: an APP fact, not a HOST fact) ---------------------

// An app that keeps no index does not get a verb that could only fail. dlc is
// exactly this app — no collection to list — and it is the reason registration
// is conditional at all.
func TestNoRebuilderMeansNoIndexVerb(t *testing.T) {
	cleanEnv(t)
	RegisterAll()

	if _, ok := registry[MethodRebuildIndex]; ok {
		t.Fatal("rebuild-index registered for an app that supplied no rebuilder")
	}
	if res := Execute(MethodRebuildIndex, nil); res.Success {
		t.Fatal("rebuild-index dispatched for an app that keeps no index")
	}
}

// And an app that keeps one gets it — whichever order it happens to call
// SetIndexRebuilder and RegisterAll in. Ordering inside init is invisible, and
// getting it wrong would cost a silently missing command.
func TestIndexVerbRegistersInEitherOrder(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func()
	}{
		{"rebuilder first", func() {
			SetIndexRebuilder(func() (uint32, error) { return 0, nil })
			RegisterAll()
		}},
		{"register first", func() {
			RegisterAll()
			SetIndexRebuilder(func() (uint32, error) { return 0, nil })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanEnv(t)
			tc.setup()
			if _, ok := registry[MethodRebuildIndex]; !ok {
				t.Fatal("rebuild-index not registered")
			}
		})
	}
}

// The index is stored on the filesystem, so a host that has no filesystem
// cannot serve the verb either — for a DISCOVERING app, which is the only kind
// that can observe the difference.
func TestIndexVerbFollowsTheFilesystem(t *testing.T) {
	cleanEnv(t)
	SetIndexRebuilder(func() (uint32, error) { return 0, nil })
	RegisterDiscovered()

	send(t, manifest(1, ilcv1.Availability_AVAILABILITY_PRESENT))
	if _, ok := registry[MethodRebuildIndex]; !ok {
		t.Fatal("rebuild-index missing on a host with a filesystem")
	}
	send(t, manifest(2, ilcv1.Availability_AVAILABILITY_ABSENT))
	if _, ok := registry[MethodRebuildIndex]; ok {
		t.Fatal("rebuild-index survived the filesystem going away")
	}
	// And back, for the same reason export-fs comes back: a browser can regain
	// an OPFS handle it lost.
	send(t, manifest(3, ilcv1.Availability_AVAILABILITY_PRESENT))
	if _, ok := registry[MethodRebuildIndex]; !ok {
		t.Fatal("rebuild-index did not return with the filesystem")
	}
}

// Registration is now block by block rather than "everything in the map", which
// is a shape that can silently drop a verb: a future capability whose ids land
// outside every known block would be generated, dispatchable in theory, and
// registered by nobody. This is the check that turns that into a failure.
func TestBlocksCoverEveryHandler(t *testing.T) {
	blocks := [][2]uint32{
		{blockCoreLo, blockCoreHi},
		{blockFilesystemLo, blockFilesystemHi},
		{blockIndexLo, blockIndexHi},
	}
	for method := range platformHandlers() {
		covered := false
		for _, b := range blocks {
			if method >= b[0] && method <= b[1] {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("method %d falls in no registration block — it would never be registered", method)
		}
	}
}

// --- the verb itself --------------------------------------------------------

func TestRebuildIndexReportsTheCount(t *testing.T) {
	cleanEnv(t)
	SetIndexRebuilder(func() (uint32, error) { return 7, nil })
	RegisterAll()

	res := Execute(MethodRebuildIndex, nil)
	if !res.Success {
		t.Fatalf("rebuild-index: %s", res.Err)
	}
	var resp ilcv1.RebuildIndexResponse
	if err := resp.UnmarshalVT(res.Output); err != nil {
		t.Fatal(err)
	}
	if resp.GetEntries() != 7 {
		t.Fatalf("entries = %d, want 7", resp.GetEntries())
	}
}

// A failing rebuild fails the command. The alternative — reporting zero entries
// and success — would tell a caller their index is empty when it is actually
// unknown.
func TestRebuildIndexSurfacesTheAppsError(t *testing.T) {
	cleanEnv(t)
	SetIndexRebuilder(func() (uint32, error) { return 0, errors.New("records/ is unreadable") })
	RegisterAll()

	res := Execute(MethodRebuildIndex, nil)
	if res.Success {
		t.Fatal("rebuild-index succeeded despite the rebuilder failing")
	}
	if !strings.Contains(res.Err, "records/ is unreadable") {
		t.Fatalf("the app's reason did not survive: %q", res.Err)
	}
}

// Rebuilding emits NOTHING. It is derived data: a rebuild that announced itself
// would make every subscriber re-read to find the same records it already had.
func TestRebuildIndexIsSilent(t *testing.T) {
	cleanEnv(t)
	seen := recordEvents(t)
	SetIndexRebuilder(func() (uint32, error) { return 3, nil })
	RegisterAll()

	if res := Execute(MethodRebuildIndex, nil); !res.Success {
		t.Fatalf("rebuild-index: %s", res.Err)
	}
	if len(*seen) != 0 {
		t.Fatalf("rebuild-index emitted %v", *seen)
	}
}

// --- D5: the index never travels -------------------------------------------

// The whole point of a derived thing is that it is not state. An index in a
// bundle would make two identical stores compare unequal, and would restore a
// projection built from someone else's files.
func TestIndexIsExcludedFromABundle(t *testing.T) {
	cleanEnv(t)
	inTempRoot(t)
	RegisterAll()

	if err := WriteFile("records/a.json", []byte(`{"id":"a"}`)); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(IndexDir+"/records", []byte("projection bytes")); err != nil {
		t.Fatal(err)
	}

	res := Execute(MethodExportFs, mustMarshal(t, &ilcv1.ExportFsRequest{}))
	if !res.Success {
		t.Fatalf("export-fs: %s", res.Err)
	}
	var resp ilcv1.ExportFsResponse
	if err := resp.UnmarshalVT(res.Output); err != nil {
		t.Fatal(err)
	}
	bundle := string(resp.GetBundle())
	if !strings.Contains(bundle, "records/a.json") && !strings.Contains(bundle, "a.json") {
		t.Fatalf("the record itself is missing from the bundle: %s", bundle)
	}
	if strings.Contains(bundle, IndexDir) {
		t.Fatalf("the index travelled in the bundle: %s", bundle)
	}
}

// Asking for the index directly gets nothing, rather than walking straight past
// the exclusion above — which skips it as a CHILD and would never see it as the
// root of the walk.
func TestExportingTheIndexDirectlyYieldsNothing(t *testing.T) {
	cleanEnv(t)
	inTempRoot(t)

	if err := WriteFile(IndexDir+"/records", []byte("projection bytes")); err != nil {
		t.Fatal(err)
	}
	tree, err := ReadTree(filepath.Join(Root(), IndexDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.entries) != 0 {
		t.Fatalf("reading the index directory returned %d entries", len(tree.entries))
	}
}

// A user's own directory that happens to share the name is NOT the platform's
// index and must survive. The exclusion matches one path, not one name.
func TestOnlyThePlatformsIndexIsExcluded(t *testing.T) {
	cleanEnv(t)
	root := inTempRoot(t)

	nested := filepath.Join(root, "mine", IndexDir)
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "keep.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	tree, err := ReadTree(Root())
	if err != nil {
		t.Fatal(err)
	}
	mine, ok := tree.entries["mine"]
	if !ok {
		t.Fatal("the user's own directory was excluded")
	}
	if _, ok := mine.entries[IndexDir]; !ok {
		t.Fatalf("a user directory named %s was swallowed by the index exclusion", IndexDir)
	}
}

func mustMarshal(t *testing.T, m interface{ MarshalVT() ([]byte, error) }) []byte {
	t.Helper()
	b, err := m.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	return b
}
