// Regenerates spikes/proto/golden.{hex,json} from the Spike 2 fixture.
package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	spikev1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/spike/v1"
)

func main() {
	msg := &spikev1.SpikeMessage{Name: "spike", Count: 42, Ok: true}
	bin, err := msg.MarshalVT()
	must(err)
	js, err := msg.MarshalJSON()
	must(err)

	dir := filepath.Join("spikes", "proto")
	must(os.WriteFile(filepath.Join(dir, "golden.hex"), []byte(hex.EncodeToString(bin)+"\n"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "golden.json"), append(bytesTrimRightNL(js), '\n'), 0o644))
	fmt.Printf("wrote %s/golden.hex (%d bytes wire)\n", dir, len(bin))
	fmt.Printf("wrote %s/golden.json (%s)\n", dir, string(js))
}

func bytesTrimRightNL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
