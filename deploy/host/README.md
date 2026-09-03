# Mac Host Codex App-Server Bridge

Use this optional setup when RelayDeck runs in Docker Desktop on this Mac and
you want the container to use the Mac's official Codex app-server. The bridge
does not mount or copy `~/.codex` into Docker. It starts a host app-server on
`127.0.0.1:19881`; RelayDeck reaches it through `host.docker.internal` with a
dedicated capability token.

The host process only listens on the Mac loopback address. Devices on your LAN
can access RelayDeck according to its normal port configuration, but cannot
directly connect to the host Codex app-server.

## Prerequisites

- Docker Desktop is running on this Mac.
- The `codex` command is installed for the same macOS user that runs this
  installer and has an active Codex session.
- RelayDeck's `deploy/.env` exists and is mode `0600`.

## Install

From the RelayDeck repository root, run:

```bash
./deploy/host/install-codex-app-server-bridge.sh --deploy-dir "$PWD/deploy"
docker compose --project-name relaydeck --project-directory "$PWD/deploy" --env-file "$PWD/deploy/.env" -f "$PWD/deploy/docker-compose.local.yml" up -d --force-recreate --no-deps relaydeck
```

The installer creates a private token at
`~/.relaydeck/codex-app-server/bridge.token`, updates only the ignored
`deploy/.env` bridge settings, and installs a per-user launchd agent. It never
prints the token and never reads or mounts the complete `~/.codex` directory.

## Verify

```bash
launchctl print "gui/$(id -u)/com.relaydeck.codex-app-server"
curl --fail http://127.0.0.1:19881/readyz
docker compose --project-name relaydeck --project-directory "$PWD/deploy" --env-file "$PWD/deploy/.env" -f "$PWD/deploy/docker-compose.local.yml" ps
```

After the RelayDeck container is healthy, open the RelayDeck admin panel and
start **官方 app-server 登录** with the device-code method. Complete the
verification in the displayed browser flow, then create the account.

## Diagnose

- If `/readyz` fails, inspect
  `~/.relaydeck/codex-app-server/stderr.log` and run
  `launchctl print "gui/$(id -u)/com.relaydeck.codex-app-server"`.
- If RelayDeck reports the host app-server as unavailable, confirm the launchd
  agent and `/readyz` first, then recreate only the `relaydeck` container.
- If the Mac sleeps, shuts down, or its user session is unavailable, the
  host-managed app-server cannot serve RelayDeck until the Mac and launch agent
  are available again.

## Rotate the Bridge Token

Stop the launch agent, remove only the dedicated bridge token, rerun the
installer, then recreate only the RelayDeck application container. Do not
remove `~/.codex` or RelayDeck data directories.

```bash
launchctl bootout "gui/$(id -u)/com.relaydeck.codex-app-server"
rm -f "$HOME/.relaydeck/codex-app-server/bridge.token"
./deploy/host/install-codex-app-server-bridge.sh --deploy-dir "$PWD/deploy"
docker compose --project-name relaydeck --project-directory "$PWD/deploy" --env-file "$PWD/deploy/.env" -f "$PWD/deploy/docker-compose.local.yml" up -d --force-recreate --no-deps relaydeck
```

## Roll Back

Stop the launch agent, remove the two bridge settings from the ignored
`deploy/.env`, then recreate only the RelayDeck application container. This
does not change PostgreSQL, Redis, existing accounts, or your Codex session.

```bash
launchctl bootout "gui/$(id -u)/com.relaydeck.codex-app-server"
sed -i '' \
  -e '/^CODEX_APP_SERVER_REMOTE_URL=/d' \
  -e '/^CODEX_APP_SERVER_REMOTE_TOKEN_FILE_HOST=/d' \
  "$PWD/deploy/.env"
docker compose --project-name relaydeck --project-directory "$PWD/deploy" --env-file "$PWD/deploy/.env" -f "$PWD/deploy/docker-compose.local.yml" up -d --force-recreate --no-deps relaydeck
```
