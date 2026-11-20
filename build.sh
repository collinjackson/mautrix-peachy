#!/bin/sh
# Build the bridge. Needs Rust and Go.
#
# Peachy's MLS engine, which the Peachy client (peachy-go) links, comes from
# peachy-protocol (https://github.com/peachysocial/peachy-protocol, MIT) at the
# commit peachy-go names in its peachy-protocol.commit: $PEACHY_PROTOCOL when
# set, else a clone this makes in target/peachy-protocol.
#
# On macOS the linker warns that some Rust objects were built for a newer
# macOS than Go links for; the warnings are harmless.
set -e
cd "$(dirname "$0")"
client=$(go list -m -f '{{.Dir}}' github.com/collinjackson/peachy-go)
if [ -z "$client" ]; then
	go mod download github.com/collinjackson/peachy-go
	client=$(go list -m -f '{{.Dir}}' github.com/collinjackson/peachy-go)
fi
commit=$(cat "$client/peachy-protocol.commit")
protocol=${PEACHY_PROTOCOL:-target/peachy-protocol}
if [ ! -d "$protocol" ]; then
	git clone -q https://github.com/peachysocial/peachy-protocol.git "$protocol"
	git -C "$protocol" checkout -q "$commit"
fi
if [ "$(git -C "$protocol" rev-parse HEAD 2>/dev/null)" != "$commit" ]; then
	echo "build.sh: $protocol isn't at $commit, the commit peachy-go is built for" >&2
	exit 1
fi
(cd "$protocol/mls" && cargo build --release -p peachy-mls-cabi)

# target/lib holds the engine's static library twice: as itself, which
# peachy-go links (-lpeachy_mls_cabi; never the .dylib beside it), and as
# libsqlite3.a. That keeps one SQLite in the binary, the SQLCipher compiled
# into the engine: go-sqlite3 (the bridge's own database) would otherwise
# compile its plain SQLite, whose symbols win the link, and the store's
# encryption then fails ("no such function: sqlcipher_export"). With the
# libsqlite3 tag it links -lsqlite3 instead. Unkeyed, SQLCipher is a plain
# SQLite, so the bridge database is unchanged.
engine="$(cd "$protocol/mls/target/release" && pwd)/libpeachy_mls_cabi.a"
mkdir -p target/lib
ln -sf "$engine" target/lib/libpeachy_mls_cabi.a
ln -sf "$engine" target/lib/libsqlite3.a
CGO_LDFLAGS="-L$(pwd)/target/lib ${CGO_LDFLAGS:-}" \
	go build -tags "goolm libsqlite3" -o mautrix-peachy "$@" ./cmd/mautrix-peachy
# Refuse a binary with a second SQLite or none of SQLCipher.
# (The SQL function's name, which stays in a stripped binary.)
if ! grep -q sqlcipher_export mautrix-peachy; then
	echo "build.sh: no SQLCipher in mautrix-peachy (a second SQLite won the link)" >&2
	exit 1
fi
