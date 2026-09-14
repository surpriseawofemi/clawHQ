# Local patches to github.com/a3tai/openclaw-go

Vendored from upstream `v1.20260325.0`. Upstream targets an older gateway protocol
than OpenClaw 2026.9.4 ships, so three changes are applied. Each was verified against
a live 2026.9.4 gateway; without them, connect fails outright.

## 1. `protocol/protocol.go` — protocol version 3 to 4

```go
const ProtocolVersion = 4
```

Upstream sends `minProtocol: 3, maxProtocol: 3`. A 2026.9.4 gateway rejects that range
with `INVALID_REQUEST: protocol mismatch`.

## 2. `protocol/protocol.go` — `AuthParams` gains two fields

```go
BootstrapToken string `json:"bootstrapToken,omitempty"`
DeviceToken    string `json:"deviceToken,omitempty"`
```

The gateway's connect `auth` object has four slots — `token`, `bootstrapToken`,
`deviceToken`, `password` — and treats them differently. Upstream only modelled
`token`/`password`, so a pairing token sent as `auth.token` is rejected with
`unauthorized: gateway token mismatch`.

## 3. `gateway/client.go` + `gateway/options.go` — auth slot selection

Adds `WithBootstrapToken` / `WithDeviceToken`, routes each credential to its correct
slot, and signs the device payload with whichever token is actually presented
(upstream always signed `opts.token`). A signature over the wrong token fails
device auth even when the slot is right.

## Upstreaming

These are not ClawDesk-specific and belong upstream. Re-check on every openclaw-go
release; if upstream adopts protocol 4 and the full auth shape, drop this directory
and depend on the module directly.
