#!/bin/sh

set -eu

usage() {
	printf '%s\n' "Usage: $0 --deploy-dir /absolute/path/to/RelayDeck/deploy"
}

deploy_dir=""
while [ "$#" -gt 0 ]; do
	case "$1" in
		--deploy-dir)
			[ "$#" -ge 2 ] || { usage >&2; exit 2; }
			deploy_dir=$2
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			usage >&2
			exit 2
			;;
	esac
done

[ -n "$deploy_dir" ] || { usage >&2; exit 2; }
deploy_dir=$(cd "$deploy_dir" && pwd -P)
env_file="$deploy_dir/.env"
[ -f "$env_file" ] || { printf '%s\n' "RelayDeck deployment .env not found: $env_file" >&2; exit 1; }

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)
bridge_script="$script_dir/codex-app-server-bridge.sh"
plist_template="$script_dir/com.relaydeck.codex-app-server.plist.template"
[ -x "$bridge_script" ] || { printf '%s\n' "Bridge runner is not executable: $bridge_script" >&2; exit 1; }
[ -f "$plist_template" ] || { printf '%s\n' "Bridge launchd template is missing: $plist_template" >&2; exit 1; }

bridge_dir="${HOME:?HOME is required}/.relaydeck/codex-app-server"
token_file="$bridge_dir/bridge.token"
codex_home="${CODEX_HOME:-$HOME/.codex}"
launch_agents_dir="$HOME/Library/LaunchAgents"
plist_path="$launch_agents_dir/com.relaydeck.codex-app-server.plist"
launch_domain="gui/$(id -u)"
launch_label="$launch_domain/com.relaydeck.codex-app-server"

umask 077
mkdir -p "$bridge_dir" "$launch_agents_dir"
chmod 700 "$bridge_dir"

if [ -n "${CODEX_BIN:-}" ]; then
	codex_bin="$CODEX_BIN"
else
	PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"
	export PATH
	codex_bin=$(command -v codex || true)
fi
[ -n "$codex_bin" ] && [ -x "$codex_bin" ] || {
	printf '%s\n' "RelayDeck Codex bridge cannot find an executable Codex CLI; set CODEX_BIN." >&2
	exit 1
}
for required_flag in --listen --ws-auth --ws-token-file; do
	if ! "$codex_bin" app-server --help 2>&1 | grep -E -- "(^|[[:space:]])${required_flag}([[:space:]=]|$)" >/dev/null; then
		printf '%s\n' "RelayDeck Codex bridge requires an app-server supporting $required_flag." >&2
		exit 1
	fi
done

if [ ! -e "$token_file" ]; then
	token_tmp=$(mktemp "$bridge_dir/bridge.token.XXXXXX")
	openssl rand -hex 32 > "$token_tmp"
	chmod 600 "$token_tmp"
	mv "$token_tmp" "$token_file"
fi
[ -s "$token_file" ] || { printf '%s\n' "Bridge token file is empty: $token_file" >&2; exit 1; }
chmod 600 "$token_file"

update_env_value() {
	key=$1
	value=$2
	tmp_file=$(mktemp "$deploy_dir/.env.bridge.XXXXXX")
	awk -v key="$key" -v value="$value" '
		$0 ~ "^" key "=" { print key "=" value; found = 1; next }
		{ print }
		END { if (!found) print key "=" value }
	' "$env_file" > "$tmp_file"
	chmod 600 "$tmp_file"
	mv "$tmp_file" "$env_file"
}

update_env_value "CODEX_APP_SERVER_REMOTE_URL" "ws://host.docker.internal:19881"
update_env_value "CODEX_APP_SERVER_REMOTE_TOKEN_FILE_HOST" "$token_file"

escape_sed() {
	printf '%s' "$1" | sed 's/[\\&|]/\\&/g'
}

tmp_plist=$(mktemp "$launch_agents_dir/com.relaydeck.codex-app-server.XXXXXX")
sed \
	-e "s|__BRIDGE_SCRIPT__|$(escape_sed "$bridge_script")|g" \
	-e "s|__USER_HOME__|$(escape_sed "$HOME")|g" \
	-e "s|__CODEX_HOME__|$(escape_sed "$codex_home")|g" \
	-e "s|__CODEX_BIN__|$(escape_sed "$codex_bin")|g" \
	-e "s|__BRIDGE_DIR__|$(escape_sed "$bridge_dir")|g" \
	"$plist_template" > "$tmp_plist"
chmod 600 "$tmp_plist"
mv "$tmp_plist" "$plist_path"

launchctl bootout "$launch_label" >/dev/null 2>&1 || true
launchctl bootstrap "$launch_domain" "$plist_path"

printf '%s\n' "RelayDeck host Codex app-server bridge installed."
printf '%s\n' "Verify with: launchctl print $launch_label"
printf '%s\n' "Verify readiness with: curl --fail http://127.0.0.1:19881/readyz"
