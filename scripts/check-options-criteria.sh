#!/usr/bin/env bash
# Verify spikes/options against the three Decision 29 pass criteria.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

fail=0
pass() { echo "  [PASS] $*"; }
failc() { echo "  [FAIL] $*"; fail=1; }

echo "======== C1: descriptor.proto + extend → buf lint + go-lite generate ========"
if (cd proto && buf lint >/dev/null && buf generate >/dev/null); then
  pass "buf lint + buf generate"
else
  failc "buf lint / generate errored"
fi
if grep -q 'google/protobuf/descriptor.proto' proto/devalbo/options/v1/options.proto \
  && grep -q 'extend google.protobuf.MethodOptions' proto/devalbo/options/v1/options.proto; then
  pass "schema imports descriptor.proto and extends MethodOptions"
else
  failc "options.proto missing import/extend"
fi

echo
echo "======== C2: TinyGo wasip2; no google.golang.org/protobuf in engine graph ========"
if tinygo build -target=wasip2 --wit-package ./wit --wit-world engine \
    -o /tmp/options-guest.wasm ./spikes/options; then
  pass "tinygo build -target=wasip2 ./spikes/options"
else
  failc "tinygo build failed"
fi

# Packages reachable from the guest (standard go list mirrors TinyGo's module graph)
DEPS=$(go list -deps ./spikes/options)
if echo "$DEPS" | grep -q '^google.golang.org/protobuf'; then
  failc "engine import graph includes google.golang.org/protobuf"
  echo "$DEPS" | grep '^google.golang.org/protobuf' | sed 's/^/    /'
else
  pass "no google.golang.org/protobuf in go list -deps ./spikes/options"
fi

if echo "$DEPS" | grep -q 'protobuf-go-lite/types/descriptorpb'; then
  echo "  [NOTE] blank-import of go-lite types/descriptorpb is present (from options.pb.go)"
  echo "         — not google.golang.org/protobuf; check size if it bites later"
else
  pass "no go-lite descriptorpb in guest deps either"
fi

# Confirm go-lite descriptorpb does not itself import google.golang.org/protobuf
if go list -deps github.com/aperturerobotics/protobuf-go-lite/types/descriptorpb 2>/dev/null \
    | grep -q '^google.golang.org/protobuf'; then
  failc "go-lite types/descriptorpb pulls google.golang.org/protobuf"
else
  pass "go-lite types/descriptorpb has no google.golang.org/protobuf dep"
fi

echo
echo "======== C3: FileDescriptorSet — protoreflect reads method_id on service rpc ========"
if grep -q 'service CommandsService' proto/devalbo/optionsspike/v1/commands.proto \
  && grep -q 'option (devalbo.options.v1.method_id)' proto/devalbo/optionsspike/v1/commands.proto; then
  pass "commands.proto has service rpc with method_id option"
else
  failc "service rpc / method_id missing from commands.proto"
fi

# go-lite emits nothing for services?
if grep -qiE 'CommandsService|type.*Service|func.*Greet\(' gen/go/devalbo/optionsspike/v1/commands.pb.go; then
  echo "  [NOTE] go-lite emitted service-related symbols — plugin may read generated Go"
else
  echo "  [NOTE] go-lite emits NO service stubs — protoc-gen-dlc-registry must read FileDescriptorSet / buf image (not generated Go)"
fi

(cd proto && buf build -o /tmp/options.desc.binpb)
if go run ./spikes/options/cmd/host-introspect /tmp/options.desc.binpb; then
  pass "host-introspect reads method_id (+ field options) from FileDescriptorSet"
else
  failc "host-introspect failed"
fi

echo
echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
  echo "→ ALL THREE PASS CRITERIA GREEN"
  exit 0
fi
echo "→ ONE OR MORE CRITERIA RED"
exit 1
