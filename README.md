# eBot-CSGO-Web-Golang

> [!WARNING]
> **Disclaimer.** This is a quick move from PHP 5.4 to Go, made to get away from the many PHP vulnerabilities, among them the recent one in PHP 8.4.
>
> Most of it has probably not been tested, and large parts were written with the help of AI. Review it and test it before you rely on it.

The match admin panel for the eBot CS:GO bot, written in Go. It is a rewrite of the PHP panel [eBot-CSGO-Web](https://github.com/NPetersenDK/eBot-CSGO-Web). It runs against the same MySQL database and the same Redis as the PHP panel and the bot.

## How it differs from the PHP panel

- One static binary. No PHP, no Symfony, no PHP-FPM. The templates and static files are embedded in the binary.
- Distroless container image that runs as a non-root user (UID 65532) with a read-only root filesystem.
- Health endpoints for orchestrators: `/healthz` (liveness) and `/readyz` (checks MySQL and Redis).
- Configuration only through environment variables. Each variable also accepts a `NAME_FILE` path for secrets.
- Optional OIDC single sign-on with role mapping. Local accounts still work.
- It creates the database tables itself when the database is empty, so the PHP panel is not needed. It never alters or drops a table that exists. See "Running without the PHP panel".

## Running side by side with the PHP panel

Both panels use one MySQL database (`ebotv3` by default) and one Redis.

- The tables are the same ones. Either panel can create them on an empty database. The Go panel only creates tables that are missing and never changes one that exists, so it is safe on a database the PHP panel made.
- Users are shared. Local accounts are rows in `sf_guard_user`, the table the PHP panel uses. A user can log in to either panel with the same password.
- Sessions are separate. Logging in to one panel does not log you in to the other.
- Matches, teams, servers and everything else are the same rows in both panels.
- The bot reads matches from MySQL and sends live commands through Redis. Both panels can start and control matches.

## Quick start with Docker Compose

Requirements: Docker with the Compose plugin, and the eBot-CSGO bot image (built from [eBot-CSGO](https://github.com/NPetersenDK/eBot-CSGO), see `EBOT_BOT_IMAGE` below).

```sh
cd deploy
cp ../.env.example .env
# Replace every change-me value in .env. EBOT_SESSION_KEY needs at least 32 bytes:
#   openssl rand -base64 32
docker compose up -d --build
```

The panel is then at http://localhost:8080/.

The compose file starts MySQL 8.4, Redis 7, the bot and the Go panel. On an empty database the Go panel creates the tables and, with `EBOT_ADMIN_USER` and `EBOT_ADMIN_PASSWORD` set, the first admin account. The PHP panel is not needed, but it is behind the `legacy` profile if you want to run both:

```sh
docker compose --profile legacy up -d ebot-web-php
```

The bot needs a `config/config.ini` that points at the `mysql` and `redis` services and listens on `0.0.0.0`. See the comments in `deploy/docker-compose.yml`.

Demos and match logs are shared between the bot and the panel through `deploy/demos` and `deploy/logs` (`EBOT_DEMOS_DIR`, `EBOT_LOGS_DIR`). The panel mounts them read-only.

## Configuration

Every variable is read in `internal/config/config.go`. Each one also accepts `NAME_FILE`, a path to a file that holds the value. Set `NAME` or `NAME_FILE`, not both. An empty value means the default is used.

| Variable | Default | Description |
|---|---|---|
| `HTTP_ADDR` | `:8080` | Address the HTTP server listens on. |
| `EBOT_TITLE` | `eBot` | Brand shown in the page header and title. |
| `EBOT_VERSION` | (build version) | Version written to the startup log. |
| `EBOT_DB_DSN` | (empty) | Full MySQL DSN. When set, the `EBOT_DB_*` connection variables below are ignored. |
| `EBOT_DB_HOST` | `127.0.0.1` | MySQL host. |
| `EBOT_DB_PORT` | `3306` | MySQL port. |
| `EBOT_DB_NAME` | `ebotv3` | MySQL database name. |
| `EBOT_DB_USER` | `ebot` | MySQL user. |
| `EBOT_DB_PASSWORD` | (empty) | MySQL password. |
| `EBOT_REDIS_ADDR` | `127.0.0.1:6379` | Redis address, `host:port`. |
| `EBOT_REDIS_USERNAME` | (empty) | Redis ACL user name. Leave empty for the default user. |
| `EBOT_REDIS_PASSWORD` | (empty) | Redis password. |
| `EBOT_REDIS_FROM_WS` | `ebot-from-ws` | Redis list the panel pushes bot commands to. The bot reads it. |
| `EBOT_REDIS_TO_WS` | `ebot-to-ws` | Redis pub/sub channel the bot publishes live messages on. The panel reads it. |
| `EBOT_SESSION_KEY` | (none) | Key that signs session cookies. Required, at least 32 bytes. The placeholder from `.env.example` is refused. |
| `EBOT_INSECURE_COOKIES` | `false` | Plain HTTP and a random session key. Development only. |
| `EBOT_TRUST_PROXY` | `false` | Take the visitor address from the last `X-Forwarded-For` value. It turns on the login limit per address and sets the per-address cap on live connections. Enable only behind a proxy that sets the header. |
| `EBOT_TIMEZONE` | `UTC` | IANA time zone name, for example `Europe/Copenhagen`. Dates are shown and typed in this zone. They are stored as UTC. |
| `EBOT_AUTO_MIGRATE` | `true` | Create the tables that are missing at start. It never alters or drops a table that exists. Set `false` to leave the database alone. |
| `EBOT_DB_WAIT` | `60` | Seconds to wait for the database to accept connections at start. |
| `EBOT_ADMIN_USER` | (empty) | Creates this admin account when the user table is empty. Needs `EBOT_ADMIN_PASSWORD`. |
| `EBOT_ADMIN_PASSWORD` | (empty) | Password for the account above. |
| `EBOT_LOCAL_LOGIN_DISABLED` | `false` | Hides the username and password form. Needs `EBOT_OIDC_ISSUER`. |
| `EBOT_OIDC_ISSUER` | (empty) | OIDC issuer URL. Empty turns SSO off. |
| `EBOT_OIDC_CLIENT_ID` | (empty) | OIDC client ID. Required with the issuer. |
| `EBOT_OIDC_CLIENT_SECRET` | (empty) | OIDC client secret. Required with the issuer. |
| `EBOT_OIDC_REDIRECT_URL` | (empty) | Callback URL, `https://<host>/auth/oidc/callback`. Required with the issuer. |
| `EBOT_OIDC_ROLES_CLAIM` | `roles` | Token claim that lists the user's roles or groups. |
| `EBOT_OIDC_ROLE_MAP` | (empty) | `claimValue=role` pairs, comma separated. Roles are `admin` and `viewer`. |
| `EBOT_OIDC_DEFAULT_ROLE` | `deny` | Role for users that match no entry in the role map. `deny` rejects them. |
| `EBOT_LOGIN_PROVIDERS` | (empty) | JSON list of extra SSO buttons: `[{"name","hint","icon"}]`. |
| `EBOT_DEMO_PATH` | `/demos` | Directory with the GOTV demos. |
| `EBOT_DEMO_DOWNLOAD` | `true` | Offer demo downloads. |
| `EBOT_LOG_MATCH_PATH` | `/logs/log_match` | Directory with the match logs, for the public match view. |
| `EBOT_LOG_MATCH_ADMIN_PATH` | `/logs/log_match_admin` | Directory with the match logs, for the admin log view. |
| `EBOT_MODE` | `net` | `net` shows the GOTV address of a server. `lan` shows the server address. |
| `EBOT_REFRESH_TIME` | `30` | Refresh interval in seconds. |
| `EBOT_MAPS` | `de_dust2,de_mirage,de_inferno,de_nuke,de_ancient,de_anubis,de_overpass,de_vertigo` | Maps offered in the match form. `tba` is always added. |
| `EBOT_RULES` | `rules` | Rule sets offered in the match form. |
| `EBOT_DEFAULT_MAX_ROUND` | `12` | Default rounds per half in new matches. |
| `EBOT_DEFAULT_RULES` | `rules` | Default rule set in new matches. |
| `EBOT_DEFAULT_OVERTIME_ENABLE` | `false` | Overtime enabled by default in new matches. |
| `EBOT_DEFAULT_OVERTIME_MAX_ROUND` | `3` | Default overtime rounds per half. |
| `EBOT_DEFAULT_OVERTIME_START_MONEY` | `10000` | Default starting money in overtime. |
| `EBOT_DEFAULT_PASSWORD` | (empty) | Default password for new matches. |
| `EBOT_API_KEYS` | (empty) | Comma separated API keys. The `/api` routes are off when empty. |
| `EBOT_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `EBOT_LOG_FORMAT` | `text` | `text` or `json`. |

Set `EBOT_DB_DSN` or the `EBOT_DB_*` variables, not both. The DSN form takes precedence.

The compose file and the Helm chart add these variables for their own use: `MYSQL_ROOT_PASSWORD`, `EBOT_BOT_IMAGE`, `EBOT_BOT_PORT`, `EBOT_WEB_PORT`, `EBOT_PHP_WEB_IMAGE`, `EBOT_PHP_WEB_PORT`, `EBOT_ADMIN_EMAIL`, `EBOT_DEMOS_DIR` and `EBOT_LOGS_DIR`. See `.env.example`.

## Authentication

Local accounts are rows in `sf_guard_user` with the password hash that the PHP panel uses. Existing PHP panel accounts log in without changes.

SSO is optional. Set `EBOT_OIDC_ISSUER`, `EBOT_OIDC_CLIENT_ID`, `EBOT_OIDC_CLIENT_SECRET` and `EBOT_OIDC_REDIRECT_URL`. The panel reads the roles from the token claim named by `EBOT_OIDC_ROLES_CLAIM` and maps them with `EBOT_OIDC_ROLE_MAP`:

```sh
EBOT_OIDC_ROLE_MAP=ebot-admin=admin,ebot-viewer=viewer
```

- `admin` can do everything.
- `viewer` can open the admin pages but cannot change anything.
- `EBOT_OIDC_DEFAULT_ROLE=deny` (the default) rejects users that match no entry. Set it to `viewer` to let every other SSO user read the admin pages.

`EBOT_LOCAL_LOGIN_DISABLED=true` hides the password form. It needs SSO to be configured, otherwise nobody could log in.

## Running without the PHP panel

Point the panel at an empty MySQL database and start it. It creates the tables, then creates the first admin account when `EBOT_ADMIN_USER` and `EBOT_ADMIN_PASSWORD` are set and there are no users yet.

```sh
EBOT_DB_HOST=mysql EBOT_DB_NAME=ebotv3 EBOT_DB_USER=ebot EBOT_DB_PASSWORD=... \
EBOT_SESSION_KEY=... EBOT_ADMIN_USER=admin EBOT_ADMIN_PASSWORD=... ./server
```

- The tables are the ones the bot and the PHP panel use, taken from the Doctrine output of eBot-CSGO-Web (`internal/store/schema.sql`, 23 tables with their foreign keys). The bot works against them without changes.
- Only missing tables are created. A table that exists is never altered, so the panel is safe to start on a database made by the PHP panel, and starting it again does nothing.
- If creating the tables fails halfway, the tables made in that run are removed again, so the next start begins clean.
- The panel waits up to `EBOT_DB_WAIT` seconds for MySQL, so it can start together with the database.
- The database user needs `CREATE`, `ALTER` and `REFERENCES` on the database for the first start (the foreign keys are added with `ALTER TABLE`), and `DROP` to undo a failed first start. Once the tables exist, nothing is created and these rights are not used. The default user of the MySQL image has them.
- Columns are not upgraded. If the bot or the panel later needs a new column, add it by hand or recreate the tables.
- Several panels can start at once. A database lock makes them take turns.
- Set `EBOT_AUTO_MIGRATE=false` to never touch the schema, for example when the database user is read and write only.

To remake `schema.sql` after a schema change in eBot-CSGO-Web, run `php symfony doctrine:build --all-classes --sql --no-confirmation` there and copy `data/sql/schema.sql`. Do not edit the file by hand.

## Map dropdown

`EBOT_MAPS` sets the maps in the map dropdown of the match form, and `EBOT_RULES` sets the rule sets. Both are comma separated lists:

```sh
EBOT_MAPS=de_dust2,de_mirage,de_inferno
EBOT_RULES=rules,rules_lan
```

Two entries are always added after the configured maps:

- `tba`, shown as "Choose by map veto". The map is picked in game.
- `CurrentMap`, shown as "Current server map (no map change)". The bot does not run `changelevel` and stays on the map the game server is already on. Use it for workshop maps. Once the bot knows the real map it renames the entry to `CurrentMap-<map>`, and the panel shows that as `<map> (server map)`.

A workshop map can also be listed in `EBOT_MAPS` by name. The bot loads it with `host_workshop_map` when the name has an ID under `[WORKSHOP IDs]` in its `config.ini`.

## API keys

Set `EBOT_API_KEYS` to one or more comma separated keys. The `/api` routes are off until a key is set. Send a key in the `X-API-Key` header:

```sh
curl -H "X-API-Key: $KEY" http://localhost:8080/api/matches
```

`GET /api/health` and `GET /api/openapi.json` need no key.

## Security notes

- Password hashes stay `sha1(salt+password)` so the PHP panel can still verify them. This is weak if the database leaks.
- Redis must be on a private network. Bot commands are encrypted but not authenticated.
- API keys are full admin credentials, and `/api` returns RCON and match passwords on single items.
- The panel rejects quotes, semicolons and control characters in the values the bot pastes into console commands (rules, map, team name, flag, match password).
- Serve the panel over HTTPS behind a reverse proxy and keep `EBOT_INSECURE_COOKIES=false`. With secure cookies the panel also sends `Strict-Transport-Security`.
- Set `EBOT_TRUST_PROXY=true` only behind a proxy that sets `X-Forwarded-For`. Otherwise a client can fake its address. With it off, logins are limited per username and address only, and the per-address limit and the per-address cap on live connections are not used, because behind a proxy every visitor would share one address.

## Kubernetes (Helm)

The chart is in `deploy/helm/ebot-web`. It runs the panel with a read-only root filesystem, as UID 65532, with `/tmp` as an `emptyDir`. Liveness uses `/healthz` and readiness uses `/readyz`.

Secrets are never set in values. Create a Secret yourself, with the keys named in `secrets` in `values.yaml`:

```sh
kubectl -n ebot create secret generic ebot-web \
  --from-literal=session-key="$(openssl rand -base64 32)" \
  --from-literal=db-password='...'
```

Then install the chart:

```sh
helm install ebot-web deploy/helm/ebot-web -n ebot \
  --set image.repository=ghcr.io/<owner>/ebot-web \
  --set image.tag=0.1.0 \
  --set env.EBOT_DB_HOST=mysql.ebot.svc.cluster.local \
  --set env.EBOT_REDIS_ADDR=redis.ebot.svc.cluster.local:6379 \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set 'ingress.hosts[0].host=ebot.example.org'
```

Use `httpRoute.enabled=true` with `httpRoute.parentRefs` for the Gateway API. Demos and logs are mounted read-only from an existing PersistentVolumeClaim (`volumes.<name>.existingClaim`) or a node directory (`volumes.<name>.hostPath`). `hostAliases` can pin an OIDC issuer host to an internal address.

## Container image

The image is built from the `Dockerfile` in this repository and published to `ghcr.io/<owner>/<repo>` when a `v*` tag is pushed. Each image is verified first (`go vet`, `go test -race`, `govulncheck`).

```sh
docker build --build-arg VERSION=0.1.0 -t ebot-web:0.1.0 .
```

## Development

Run the panel against a local database with plain HTTP:

```sh
export EBOT_DB_HOST=127.0.0.1 EBOT_DB_USER=ebot EBOT_DB_PASSWORD=... EBOT_DB_NAME=ebotv3
export EBOT_REDIS_ADDR=127.0.0.1:6379
EBOT_INSECURE_COOKIES=true go run ./cmd/server
```

`EBOT_INSECURE_COOKIES=true` allows plain HTTP cookies and generates a random session key when `EBOT_SESSION_KEY` is empty. Do not use it in production.

Tests:

```sh
go vet ./...
go test -race ./...
```

The integration tests need a MySQL database. They run only when `EBOT_TEST_DSN` is set, for example:

```sh
EBOT_TEST_DSN='ebot:secret@tcp(127.0.0.1:3306)/ebot_test?parseTime=true' go test ./internal/store/...
```

Point `EBOT_TEST_DSN` at a database that the tests may change. Most tests create their own small schema from `internal/store/testdata`. The migration tests build the real schema in an empty database named after `EBOT_TEST_DSN` with a suffix.

## Project layout

| Path | Contents |
|---|---|
| `cmd/server` | Entry point. Loads the config, opens MySQL and Redis, starts the HTTP server. |
| `internal/config` | Environment variables and defaults. |
| `internal/store` | All SQL, and the embedded schema (`schema.sql`) that creates the tables. |
| `internal/auth` | Local login, OIDC, roles and sessions. |
| `internal/web` | Pages and form handlers. |
| `internal/live` | Server-sent events for live match updates. |
| `internal/ebotcmd` | Sends live commands to the bot through Redis. |
| `internal/api` | The `/api` routes. |
| `web/templates` | HTML templates, embedded in the binary. |
| `web/static` | CSS and JavaScript, embedded in the binary. |
| `deploy` | Docker Compose file and Helm chart. |

## Not in this version yet

- Statistics pages.
- Heatmaps.
- Live map.
- Widgets and JSON exports.
- Toornament import and export.

## License

GPL-3.0-or-later. See `LICENSE`. Copyright (C) 2026 eBot-CSGO-Web-Golang contributors.
