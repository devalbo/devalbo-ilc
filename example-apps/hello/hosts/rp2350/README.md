# hosts/rp2350 — inherited, not scaffolded

There is no host code here, and there is not meant to be.

On the badge the HOST IS THE FIRMWARE (`dlc-platform/embedded/rp2350`): one
app-agnostic loader that runs whatever payload is in flash, for apps it was never
built for. An app does not ship a badge slot the way it ships `hosts/native` or
`hosts/web` — it ships a component, and the world drives it.

This directory exists so `dlc.toml` can declare the tier, which is what makes
`dlc build rp2350` produce the component the loader AOT-compiles.

## Why hello targets this tier

It is the app that proves the world does not own time. It sleeps between ticks
and decides its own interval, which is app logic that would otherwise have to
live in the shell — and a shell holding app logic is the thing
`docs/SESSION-AND-SURFACE-PLAN.md` exists to avoid.

    # from the repo root, once this app's component is built
    make badge-cwasm APP=hello
    make badge-payload PAYLOADS="hello=build/hello.pulley32.cwasm"
