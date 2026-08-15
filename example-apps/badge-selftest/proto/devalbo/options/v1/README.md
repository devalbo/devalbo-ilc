# Vendored from the ILC platform

`options.proto` is a **byte-for-byte copy** of `proto/devalbo/options/v1/options.proto`
in [devalbo-ilc](https://github.com/devalbo/devalbo-ilc). It defines `method_id` and the
arg-metadata options (`help` / `required` / `default` / `short`) that your `commands.proto`
imports.

**Do not edit it.** The platform reads these options; an edited copy would generate against
options the platform does not actually understand.

It travels with the scaffold only because the platform is not published to a schema registry
yet. When it is, delete this directory and add the registry dependency to `proto/buf.yaml`.

Upstream keeps the copy honest: `make sync-template-proto` refreshes it, and a Go test fails
if the two ever differ.
