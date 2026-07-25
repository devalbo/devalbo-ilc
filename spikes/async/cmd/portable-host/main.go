// portable-host — WAMR-shaped host for Spike 5 portable track (matrix F-P / P1.*).
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

type caseResult struct {
	id, status, detail string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: portable-host <engine.core.wasm> [ms]")
		os.Exit(2)
	}
	wasmPath := os.Args[1]
	ms := uint32(50)
	if len(os.Args) >= 3 {
		var v uint64
		if _, err := fmt.Sscanf(os.Args[2], "%d", &v); err != nil {
			fmt.Fprintln(os.Stderr, "bad ms:", os.Args[2])
			os.Exit(2)
		}
		ms = uint32(v)
	}

	wasm, err := os.ReadFile(wasmPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx := context.Background()
	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		fmt.Fprintln(os.Stderr, "wasi:", err)
		os.Exit(1)
	}

	_, err = r.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(_ context.Context, m uint32) uint32 {
			time.Sleep(time.Duration(m) * time.Millisecond)
			return m
		}).
		Export("host_delay").
		Instantiate(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "host module:", err)
		os.Exit(1)
	}

	mod, err := instantiateTinyGo(ctx, r, wasm)
	if err != nil {
		fmt.Fprintln(os.Stderr, "instantiate:", err)
		os.Exit(1)
	}
	defer mod.Close(ctx)

	var cases []caseResult
	rec := func(id, status, detail string) {
		cases = append(cases, caseResult{id, status, detail})
		suffix := ""
		if detail != "" {
			suffix = " — " + detail
		}
		fmt.Printf("  [%s] %s%s\n", status, id, suffix)
	}
	skip := func(ids []string, because string) {
		for _, id := range ids {
			rec(id, "SKIP", because)
		}
	}

	runWait := mod.ExportedFunction("run_wait")
	if runWait == nil {
		rec("P1.1", "FAIL", "missing export run_wait")
		skip([]string{"P1.2", "P1.3"}, "blocked by P1.1")
		printPortableRollup(cases)
		os.Exit(1)
	}

	start := time.Now()
	results, err := runWait.Call(ctx, uint64(ms))
	elapsed := time.Since(start)
	if err != nil {
		rec("P1.1", "FAIL", err.Error())
		skip([]string{"P1.2", "P1.3"}, "blocked by P1.1")
		printPortableRollup(cases)
		os.Exit(1)
	}
	rec("P1.1", "PASS", "Call returned")

	if len(results) == 1 && uint32(results[0]) == ms {
		rec("P1.2", "PASS", fmt.Sprintf("got %d", ms))
	} else {
		rec("P1.2", "FAIL", fmt.Sprintf("got %v want %d", results, ms))
	}

	if elapsed >= time.Duration(ms)*time.Millisecond {
		rec("P1.3", "PASS", fmt.Sprintf("elapsed_ms=%d", elapsed.Milliseconds()))
	} else {
		rec("P1.3", "FAIL", fmt.Sprintf("elapsed %v < %dms", elapsed, ms))
	}

	printPortableRollup(cases)
	for _, c := range cases {
		if c.status == "FAIL" {
			os.Exit(1)
		}
	}
}

func printPortableRollup(cases []caseResult) {
	fmt.Println("matrix:")
	for _, c := range cases {
		line := "  " + c.id + "\t" + c.status
		if c.detail != "" {
			line += "\t" + c.detail
		}
		fmt.Println(line)
	}
	allPass := true
	for _, c := range cases {
		if c.status != "PASS" {
			allPass = false
			break
		}
	}
	if allPass {
		fmt.Println("PORTABLE=GREEN")
	} else {
		fmt.Println("PORTABLE=RED")
	}
}

func instantiateTinyGo(ctx context.Context, r wazero.Runtime, wasm []byte) (api.Module, error) {
	var last error
	for _, starts := range [][]string{{"_initialize"}, {"_start"}, {}} {
		cfg := wazero.NewModuleConfig()
		if len(starts) > 0 {
			cfg = cfg.WithStartFunctions(starts...)
		}
		mod, err := r.InstantiateWithConfig(ctx, wasm, cfg)
		if err == nil {
			return mod, nil
		}
		last = err
	}
	return nil, last
}
