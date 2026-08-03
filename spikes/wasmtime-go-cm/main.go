// Can the CLI tier run the SAME component the browser runs?
//
// Decision 26 links the engine natively "to sidestep the wasmtime-go
// Component-Model gap". wasmtime-go is now at v47 and ships a component API, so
// that justification is worth re-testing rather than inheriting.
//
// This probe walks the four things a host must do, and reports which one stops
// it: load a component, provide its imports, instantiate it, call an export.
package main

import (
	"fmt"
	"os"

	"github.com/bytecodealliance/wasmtime-go/v47"
)

func main() {
	path := "../../engine.component.wasm"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	wasm, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("cannot read component:", err)
		os.Exit(2)
	}

	engine := wasmtime.NewEngine()

	// 1. LOAD
	component, err := wasmtime.NewComponent(engine, wasm)
	if err != nil {
		fmt.Println("1. load:              FAIL —", err)
		os.Exit(1)
	}
	fmt.Printf("1. load:              OK (%d bytes)\n", len(wasm))

	// 2. INSPECT — what does this component demand of a host?
	ct := component.Type()
	fmt.Printf("2. introspect:        OK — %d imports, %d exports\n", ct.ImportCount(), ct.ExportCount())
	for i := 0; i < ct.ImportCount(); i++ {
		name, _ := ct.ImportNth(i)
		fmt.Printf("      needs import: %s\n", name)
	}
	for i := 0; i < ct.ExportCount(); i++ {
		name, _ := ct.ExportNth(i)
		fmt.Printf("      offers export: %s\n", name)
	}

	// 3. PROVIDE IMPORTS + INSTANTIATE
	//
	// The only thing a ComponentLinker can do with an import is turn it into a
	// trap: there is no method to bind a host function, and no WASI 0.2 support
	// (the source says so — "TODO: WASIp2 / wasi:http integration"). So this
	// instantiates an engine whose every capability aborts on use.
	linker := wasmtime.NewComponentLinker(engine)
	if err := linker.DefineUnknownImportsAsTraps(component); err != nil {
		fmt.Println("3. stub imports:      FAIL —", err)
		os.Exit(1)
	}
	store := wasmtime.NewStore(engine)
	if _, err := linker.Instantiate(store, component); err != nil {
		fmt.Println("3. instantiate:       FAIL —", err)
	} else {
		fmt.Println("3. instantiate:       OK — but every import is a trap")
	}

	// 4. CALL — the one that decides it.
	fmt.Println("4. call `execute`:    IMPOSSIBLE — v47 exposes no ComponentFunc")
	fmt.Println("      component_feat_component_model.go:20 says so itself:")
	fmt.Println("      \"TODO: ComponentFunc + value marshaling (call exported component functions\"")
}
