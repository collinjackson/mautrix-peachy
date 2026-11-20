#!/bin/sh
# Run the end-to-end test (integration/): a local Synapse, the bridge and
# peachy-go's in-memory fake Peachy server, with no real accounts and no
# network beyond 127.0.0.1.
#
#   scripts/integration.sh            build, set up Synapse, run the test
#   NO_BUILD=1 scripts/integration.sh reuse target/mautrix-peachy-test
#
# Synapse runs from a Python venv in target/synapse-venv (made once, with
# pip install matrix-synapse; $PYTHON picks the interpreter). Each run starts
# from a fresh target/integration: Synapse's config, database and log, the
# bridge's config and registration. The test starts the fake Peachy server
# and the bridge itself, and leaves the bridge's log in
# target/integration/bridge.log. Extra arguments go to go test (-run, -v).
set -e
cd "$(dirname "$0")/.."
root=$(pwd)
bin="$root/target/mautrix-peachy-test"
work="$root/target/integration"
venv="$root/target/synapse-venv"
hs_port=${SYNAPSE_PORT:-18008}
as_port=${BRIDGE_PORT:-29399}

# The bridge, linked against the engine with its test issuer (which the
# test's own Peachy devices need to found groups). Never ship this build.
if [ -z "$NO_BUILD" ]; then
	PEACHY_FEATURES=peachy-mls-cabi/test-issuer OUT="$bin" ./build.sh
fi

if ! "$venv/bin/python" -c "import synapse" 2> /dev/null; then
	rm -rf "$venv"
	if [ -z "$PYTHON" ]; then
		# Synapse supports the Python releases of the last few years; prefer
		# one of those over a brand-new default python3.
		for p in python3.13 python3.12 python3.11 python3; do
			if command -v $p > /dev/null; then PYTHON=$p; break; fi
		done
	fi
	"$PYTHON" -m venv "$venv"
	"$venv/bin/pip" install -q --upgrade pip
	"$venv/bin/pip" install -q matrix-synapse
fi

rm -rf "$work"
mkdir -p "$work/synapse" "$work/bridge"

# The bridge's config: the test points network.server at its fake server
# and the database and data_dir at its own folder, through the environment
# (env_config_prefix). The rest the bridge fills in from its example config.
cat > "$work/bridge/config.yaml" <<EOF
homeserver:
    address: http://127.0.0.1:$hs_port
    domain: localhost
appservice:
    address: http://127.0.0.1:$as_port
    hostname: 127.0.0.1
    port: $as_port
database:
    type: sqlite3-fk-wal
    uri: file:$work/bridge/peachy.db?_txlock=immediate
bridge:
    permissions:
        "localhost": admin
provisioning:
    shared_secret: integration-test-provisioning-secret
matrix:
    federate_rooms: false
network:
    server: http://127.0.0.1:1
    data_dir: $work/bridge/peachy-data
    device_name: Integration test
env_config_prefix: MAUTRIX_PEACHY_
logging:
    min_level: debug
    writers:
        - type: stdout
          format: json
EOF
"$bin" -c "$work/bridge/config.yaml" -g -r "$work/bridge/registration.yaml" > /dev/null

# Synapse: localhost only, open registration, no federation, no key servers,
# no rate limits.
cd "$work/synapse"
cat > homeserver.yaml <<EOF
server_name: localhost
pid_file: $work/synapse/homeserver.pid
listeners:
  - port: $hs_port
    bind_addresses: ['127.0.0.1']
    type: http
    tls: false
    x_forwarded: false
    resources:
      - names: [client]
        compress: false
database:
  name: sqlite3
  args:
    database: $work/synapse/homeserver.db
log_config: $work/synapse/log.yaml
media_store_path: $work/synapse/media
signing_key_path: $work/synapse/localhost.signing.key
registration_shared_secret: integration-test-registration-secret
macaroon_secret_key: integration-test-macaroon-secret
form_secret: integration-test-form-secret
enable_registration: true
enable_registration_without_verification: true
report_stats: false
trusted_key_servers: []
suppress_key_server_warning: true
federation_domain_whitelist: []
app_service_config_files:
  - $work/bridge/registration.yaml
rc_message:
  per_second: 1000
  burst_count: 1000
rc_registration:
  per_second: 1000
  burst_count: 1000
rc_login:
  address: {per_second: 1000, burst_count: 1000}
  account: {per_second: 1000, burst_count: 1000}
  failed_attempts: {per_second: 1000, burst_count: 1000}
rc_joins:
  local: {per_second: 1000, burst_count: 1000}
  remote: {per_second: 1000, burst_count: 1000}
rc_invites:
  per_room: {per_second: 1000, burst_count: 1000}
  per_user: {per_second: 1000, burst_count: 1000}
EOF
cat > log.yaml <<EOF
version: 1
formatters:
  precise:
    format: '%(asctime)s - %(name)s - %(lineno)d - %(levelname)s - %(message)s'
handlers:
  file:
    class: logging.FileHandler
    formatter: precise
    filename: $work/synapse/homeserver.log
root:
  level: INFO
  handlers: [file]
disable_existing_loggers: false
EOF
"$venv/bin/python" -m synapse.app.homeserver --config-path homeserver.yaml --generate-keys > /dev/null

stop() {
	if [ -f "$work/synapse/homeserver.pid" ]; then
		kill "$(cat "$work/synapse/homeserver.pid")" 2> /dev/null || true
	fi
}
trap stop EXIT
"$venv/bin/python" -m synapse.app.homeserver --config-path homeserver.yaml --daemonize
cd "$root"

# Wait for Synapse to answer (a minute at most).
i=0
until curl -fs "http://127.0.0.1:$hs_port/health" > /dev/null; do
	i=$((i + 1))
	if [ $i -gt 60 ]; then
		echo "integration.sh: Synapse didn't start; see $work/synapse/homeserver.log" >&2
		exit 1
	fi
	sleep 1
done

CGO_LDFLAGS="-L$root/target/lib ${CGO_LDFLAGS:-}" \
	INTEGRATION_HOMESERVER="http://127.0.0.1:$hs_port" \
	INTEGRATION_BRIDGE="$bin" \
	INTEGRATION_BRIDGE_CONFIG="$work/bridge/config.yaml" \
	INTEGRATION_BRIDGE_URL="http://127.0.0.1:$as_port" \
	INTEGRATION_LOG_DIR="$work" \
	go test -tags "integration testissuer" -count=1 -timeout 10m "$@" ./integration/
