# mautrix-peachy

A Matrix bridge for [Peachy](https://pchy.app), so Peachy chats show up in
Beeper. Built like [mautrix-signal](https://github.com/mautrix/signal): a Go
Peachy client and a `bridgev2` connector on mautrix-go, with the cryptography
left to Peachy's own Rust MLS engine through its C interface (as
mautrix-signal leaves it to libsignal). Run self-hosted through Beeper's
`bbctl`.

This is an independent open-source project, not part of the Peachy product.

## Ground rules
- **Peachy's protocol comes only from
  [peachy-protocol](https://github.com/peachysocial/peachy-protocol)** (MIT),
  through peachy-go. Don't rely on any rpc, field or behavior it doesn't
  publish.
- **Don't write to Peachy's systems** beyond what a normal user's device does.
- **Test against your own account** or a local Peachy server, never someone
  else's account.

## Layout
- The Peachy client is [peachy-go](https://github.com/collinjackson/peachy-go)
  (MPL-2.0), a Go module; a `go.work` outside this repo points at a local
  checkout while changing both. Changes to the client go there; its CLI and
  bot daemon live there too.
- `pkg/connector/`: the `bridgev2` connector (login, rooms, text and media
  both ways).
- `cmd/mautrix-peachy/`: the `mxmain` binary. `./build.sh` builds Peachy's
  MLS engine (peachy-protocol at peachy-go's `peachy-protocol.commit`), then
  the bridge (`-tags "goolm libsqlite3"`, no libolm needed).
- `.github/workflows/ci.yml`: on every push, gofmt, vet, the unit tests and
  the integration test.
- `.github/workflows/release.yml`: on a `v*` tag, builds macOS arm64 and
  Linux amd64/arm64 binaries and attaches them to the release.
- `docs/`: the user-facing getting-started pages.

## Testing without Beeper
Run a local Synapse with the bridge's registration and drive login over the
provisioning API (`/_matrix/provision/v3/login/...`). `scripts/integration.sh`
does that end to end with no real accounts: Synapse from a venv in
`target/synapse-venv`, the bridge built against the test-issuer engine
(`target/mautrix-peachy-test`, never shipped), and `integration/`
(`-tags "integration testissuer"`), which runs peachy-go's fake server
(`peachytest`), logs the bridge in and plays the other people with peachy-go
Devices.

## Conventions
- `gofmt`, `go vet -tags "goolm libsqlite3" ./...`, `go test -tags "goolm
  libsqlite3" ./...`, with `CGO_LDFLAGS=-L$PWD/target/lib` after a
  `./build.sh`.
- Commit subjects start with a verb (`Add`, `Fix`, `Update`, `Remove`).
- License: AGPL-3.0, like the other mautrix bridges, so mautrix-signal's
  code may be borrowed with attribution. Peachy's protocol files and MLS
  engine stay out of the repo.
