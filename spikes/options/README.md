# Spike — go-lite + custom options (Decision 29 gate) · ✅ GREEN

**Pass criteria** (all measured green — see [`../README.md`](../README.md)):

1. `import "google/protobuf/descriptor.proto"` + `extend MethodOptions { method_id }` → `buf lint` + go-lite generate
2. Message codecs build under TinyGo wasip2 with **no** `google.golang.org/protobuf` in the engine import graph
3. `buf build` FileDescriptorSet: host reads `method_id` off a **service rpc** (protoreflect/dynamicpb)

**Run:**

```bash
devbox run make spike-options
devbox run ./scripts/check-options-criteria.sh   # asserts C1–C3 explicitly
```

| Half | Role |
| --- | --- |
| Guest (`main.go` + harness) | TinyGo round-trip of option-bearing **messages** (options ignored at runtime) |
| Host (`cmd/host-introspect`) | Reads `method_id` + field options from descriptor set |

**Schema:** `proto/devalbo/options/v1/` (extensions) · `proto/devalbo/optionsspike/v1/` (service + messages).
