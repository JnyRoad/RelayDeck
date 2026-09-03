#!/bin/sh

set -eu

BRIDGE_PORT=19881
BRIDGE_DIR="${RELAYDECK_HOST_BRIDGE_DIR:-${HOME:?HOME is required}/.relaydeck/codex-app-server}"
TOKEN_FILE="${RELAYDECK_HOST_BRIDGE_TOKEN_FILE:-$BRIDGE_DIR/bridge.token}"

if [ ! -s "$TOKEN_FILE" ]; then
	printf '%s\n' "RelayDeck Codex bridge token file is missing or empty: $TOKEN_FILE" >&2
	exit 1
fi

token_mode=$(stat -f '%Lp' "$TOKEN_FILE" 2>/dev/null || true)
if [ "$token_mode" != "600" ]; then
	printf '%s\n' "RelayDeck Codex bridge token file must use mode 0600: $TOKEN_FILE" >&2
	exit 1
fi

PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"
export PATH

if [ -n "${CODEX_BIN:-}" ]; then
	codex_bin="$CODEX_BIN"
else
	codex_bin=$(command -v codex || true)
fi

if [ -z "$codex_bin" ] || [ ! -x "$codex_bin" ]; then
	printf '%s\n' "RelayDeck Codex bridge cannot find an executable Codex CLI; set CODEX_BIN." >&2
	exit 1
fi

# CODEX_HOME is preserved by the LaunchAgent when the administrator configured
# one; otherwise Codex uses its ordinary per-user default. Only the dedicated
# bridge token is shared with RelayDeck.
exec "$codex_bin" app-server \
	--listen "ws://127.0.0.1:$BRIDGE_PORT" \
	--ws-auth capability-token \
	--ws-token-file "$TOKEN_FILE"
