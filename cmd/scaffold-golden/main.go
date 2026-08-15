// scaffold-golden — the §11 golden FS snapshot for `dlc new`.
//
//	scaffold-golden        write the snapshot to stdout
//	scaffold-golden -check compare against verify/scaffold/golden.bft.json
//
// WHY A HASH MANIFEST AND NOT THE BFT BUNDLE ITSELF: the bundle is the obvious
// choice — `export-fs` already produces one deterministically (§7.3) — and it
// was the first attempt. But BFT stores file contents as JSON-escaped strings,
// so a 3 KB AGENTS.md lands as one enormous line, and the review diff that was
// the whole justification becomes unreadable. A manifest of path + size + digest
// diffs in exactly the way a snapshot should: one short line per file, an
// addition or removal obvious at a glance.
//
// It still goes THROUGH export-fs to enumerate and read the tree, so the export
// path is exercised on every run rather than being trusted.
//
// The trade: a content change shows as a digest change, not as the change
// itself. Re-run `dlc new` and look. In exchange, an accidental file addition —
// the failure this exists to catch — is one added line instead of being buried.
//
// WHAT IT CATCHES that the invariant tests do not: an accidental ADDITION to the
// template, a file that silently stopped being emitted, and any content drift —
// a changed default, a broken token, a stray platform path. The unit tests
// assert that required files exist; this asserts the whole tree is what we meant.
//
// The snapshot is generated with a FIXED app name, module, and platform path, so
// it is a property of the template rather than of whoever ran it.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/devalbo/devalbo-ilc/dlc-platform"
	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
	"github.com/devalbo/devalbo-ilc/engine"
	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"
)

// The fixed invocation the golden describes. Changing any of these changes the
// snapshot, which is the point — they are part of what is being pinned.
const (
	goldenApp    = "golden-app"
	goldenModule = "example.com/golden-app"
	// A fixed, obviously-fake path: the real one varies per machine and would
	// make the golden unshareable.
	goldenPlatformPath = "/PLATFORM"
	goldenFile         = "verify/scaffold/golden.txt"
)

func main() {
	check := flag.Bool("check", false, "compare against the committed golden instead of printing it")
	flag.Parse()

	bundle, err := snapshot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "scaffold-golden:", err)
		os.Exit(1)
	}

	if !*check {
		os.Stdout.Write(bundle)
		return
	}

	want, err := os.ReadFile(goldenFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scaffold-golden:", err)
		os.Exit(1)
	}
	if bytes.Equal(bundle, want) {
		fmt.Println("scaffold golden: unchanged")
		return
	}
	fmt.Fprintf(os.Stderr, "scaffold-golden: the scaffold no longer matches %s\n\n"+
		"The template changed. That is fine when it is deliberate — re-bless with:\n\n"+
		"    make scaffold-golden\n\n"+
		"and review the diff: it lists every file the scaffolder emits, so an accidental\n"+
		"addition, a dropped file, or a content change is visible in one place.\n",
		goldenFile)
	os.Exit(1)
}

// snapshot scaffolds into a throwaway root and exports it as BFT.
func snapshot() ([]byte, error) {
	dir, err := os.MkdirTemp("", "dlc-golden-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	// The engine writes relative to the process working directory (§5.2), so the
	// temp dir IS the filesystem root for this run.
	prev, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err := os.Chdir(dir); err != nil {
		return nil, err
	}
	// BOOT, as a host does. Granting the root is no longer enough: dlc's engine
	// registers its filesystem verbs from the environment manifest
	// (RegisterDiscovered), so a caller that skipped it would get `export-fs:
	// unknown method_id 100` — which is what this tool started reporting the
	// moment discovery landed. Every in-process caller is a host.
	if err := platform.Boot(platform.BootOptions{
		FSRoot:         ".",
		FilesystemKind: ilcv1.FilesystemKind_FILESYSTEM_KIND_CWD,
	}); err != nil {
		return nil, err
	}
	defer os.Chdir(prev)

	newReq, err := (&dlcv1.NewRequest{
		Name:         goldenApp,
		Module:       goldenModule,
		PlatformPath: goldenPlatformPath,
		// Stated, not defaulted: the engine has no default tier set, so the
		// golden pins a layout someone chose rather than one that fell out.
		Tiers: []string{"native", "web"},
	}).MarshalVT()
	if err != nil {
		return nil, err
	}
	if r := engine.ExecuteMethod(engine.MethodNew, newReq); !r.Success {
		return nil, fmt.Errorf("new: %s", r.Err)
	}

	exportReq, err := (&ilcv1.ExportFsRequest{Prefix: goldenApp}).MarshalVT()
	if err != nil {
		return nil, err
	}
	r := engine.ExecuteMethod(platform.MethodExportFs, exportReq)
	if !r.Success {
		return nil, fmt.Errorf("export-fs: %s", r.Err)
	}
	var resp ilcv1.ExportFsResponse
	if err := resp.UnmarshalVT(r.Output); err != nil {
		return nil, err
	}
	return manifest(resp.Bundle)
}

// manifest renders one line per file: path, size, and a content digest.
func manifest(bundle []byte) ([]byte, error) {
	var root bftNode
	if err := json.Unmarshal(bundle, &root); err != nil {
		return nil, fmt.Errorf("parse bundle: %w", err)
	}
	var files []string
	if err := walk(&root, "", &files); err != nil {
		return nil, err
	}
	sort.Strings(files)

	var b bytes.Buffer
	fmt.Fprintf(&b, "# scaffold golden — `dlc new --module %s --platform-path %s %s`\n",
		goldenModule, goldenPlatformPath, goldenApp)
	fmt.Fprintf(&b, "# Regenerate with `make scaffold-golden`. A diff here is a change to what\n")
	fmt.Fprintf(&b, "# every user gets — review it like source.\n")
	fmt.Fprintf(&b, "# %-46s %8s  %s\n", "path", "bytes", "sha256")
	for _, line := range files {
		b.WriteString(line + "\n")
	}
	return b.Bytes(), nil
}

// bftNode mirrors the BFT shape; encoding/json is fine here (a native dev tool,
// never the engine).
type bftNode struct {
	Type     string             `json:"type"`
	Content  string             `json:"content"`
	Encoding string             `json:"encoding"`
	Entries  map[string]bftNode `json:"entries"`
}

func walk(n *bftNode, prefix string, out *[]string) error {
	if n.Type == "directory" {
		names := make([]string, 0, len(n.Entries))
		for name := range n.Entries {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			child := n.Entries[name]
			path := name
			if prefix != "" {
				path = prefix + "/" + name
			}
			if err := walk(&child, path, out); err != nil {
				return err
			}
		}
		return nil
	}
	content := []byte(n.Content)
	if n.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(n.Content)
		if err != nil {
			return fmt.Errorf("%s: %w", prefix, err)
		}
		content = decoded
	}
	sum := sha256.Sum256(content)
	*out = append(*out, fmt.Sprintf("%-48s %8d  %x", prefix, len(content), sum[:8]))
	return nil
}
