# Contracts

Read this before touching anything. Several people work on this repo at the same time, each on their own files. The contract files below are shared, so they must not change.

## Rules

- English only, everywhere: code, identifiers, UI text, comments, docs, commit messages.
- No em-dashes. Plain, short sentences. Comment only what the code does not say itself.
- No French names outside SQL strings. The PHP app is French: `matchs` is `matches`, `pseudo` is `nickname`, `bombe` is `bomb`, `maps_for` is `PickedBy`. Translate labels and texts from the old templates, never copy them.
- Match the old eBot pages 1:1 in structure, fields, field order and buttons per status. Only the look is new (see `web/static/styles.css`).
- Touch only the files your work package owns. Do not run `go mod tidy`. If you need a new dependency, say so in your report.
- Contract files (never edit): `CONTRACTS.md`, `internal/config/config.go`, `internal/store/types.go`, `internal/store/store.go`, `internal/auth/auth.go`, `internal/web/app.go`, `internal/web/routes.go`, `cmd/server/main.go`, `web/templates/layout.html`, `web/static/styles.css`.
- Stub files may be overwritten, but every exported signature in them must stay exactly as it is.
- If a contract is missing something, stop and say so in your report. Do not work around it.
- Test only your own package: `go build ./internal/<pkg>/... && go vet ./internal/<pkg>/... && go test -race ./internal/<pkg>/...`. Other packages may still be stubs that return `errors.ErrUnsupported`.

## Layout

| Path | Owner |
|---|---|
| `internal/store/{matches,maps,stats}.go` | WP-A |
| `internal/store/{servers,teams,seasons,ads,users}.go`, `sfguard.go` | WP-B |
| `internal/auth/{manager,handler,...}.go`, `web/templates/{login,denied}.html` | WP-C |
| `internal/live/*`, `internal/ebotcmd/*`, `web/static/live.js` | WP-D |
| `internal/web/matches.go`, `web/templates/{home,matches,match,match_form,match_score}.html` | WP-E |
| `internal/web/{crud,servers,teams,seasons,ads,users,files}.go`, `web/templates/{list,form,dashboard}.html` | WP-F |
| `Dockerfile`, `.github/`, `deploy/`, `.env.example`, `README.md`, `LICENSE` | WP-G |
| `internal/api/*` | WP-H |

## Database

The MySQL schema is shared with the PHP app and must not change. The panel creates it on an empty database from `internal/store/schema.sql` (the Doctrine output of the PHP app, never edited by hand) and never alters an existing table. Source of truth: `../eBot-CSGO-Web/config/doctrine/schema.yml` and `../eBot-CSGO-Web/plugins/sfDoctrineGuardPlugin/config/doctrine/schema.yml`.

- Every table has `id BIGINT AUTO_INCREMENT`, `created_at`, `updated_at`. There is no database default for the two timestamps: set both on every insert, `updated_at` on every update.
- Booleans are `TINYINT(1)`. Enums are `VARCHAR`.
- All SQL lives in `internal/store`. Use `database/sql` with `?` placeholders only. Never build SQL from input.
- Integration tests run against `EBOT_TEST_DSN` and skip when it is empty. Create the schema in the test from `schema.yml` by hand (a small `testdata/schema.sql`), do not depend on PHP.

## Auth

- `auth.Session{Subject, Name, Role, Source, Expires}`. Local users: `Subject` is the username, `Source` is `local`. SSO users: `Source` is `oidc`.
- `Manager.Middleware(roles)` puts the session on the request context. Handlers read it with `auth.FromContext`.
- Roles: `admin` can do everything. `viewer` (SSO only) can read admin pages but not change anything.
- Wrap routes with `auth.RequireUser` for read-only admin pages and `auth.RequireAdmin` for everything that changes state or sends a command.
- Every POST is protected by `sameOriginGuard` in `main.go`. Forms do not need a token.
- Redirect targets taken from input (`next`) must be local paths: start with a single `/`, no `//`, no `\`.

## Templates

- Files: `web/templates/<page>.html`. Each defines `{{define "content"}}`. It may also define `{{define "scripts"}}` to add `<script src="/static/...">` tags (no inline scripts, the CSP forbids them).
- `.` is `web.View`: `.Title .Brand .Path .User .IsAdmin .Flash .Cfg .Data`. Page data is `.Data`. If the data type has a `Title() string` method, it becomes the page title.
- Render with `a.Render(w, r, "page", data)` or `a.RenderStatus(w, r, code, "page", data)`. Flash: `a.SetFlash(w, "ok"|"warn"|"err", msg)` then `web.Redirect`.
- Functions: `statusLabel statusClass isRunning add sub date datetime join deref`. Add more from your own file with `registerFunc("name", fn)` in an `init()`.
- No external CSS, fonts, scripts or images. Use the classes in `styles.css`: `.card .btn .btn.primary .btn.danger .btn.ghost .btn.sm .table .badge(.ok .warn .err .muted .accent) .grid2 .form-grid .page-head .toolbar .pager .scoreboard .score .actions .console .kv .switch details.advanced .flash`.
- Escaping is automatic. Never use `template.HTML` with data that came from a user, the database or the bot.

## Routes

Public:

| Route | Notes |
|---|---|
| `GET /` | Up to 10 current matches |
| `GET /matches` | `?status=current` (default) or `archived`, `&page=`, `&season=`, `&team=`, `&q=` |
| `GET /matches/{id}` | Match view |
| `GET /matches/{id}/demo/{mapId}` | Demo download |
| `GET /live/matches` | SSE, scope `match` |
| `GET /login`, `POST /login`, `GET /logout`, `POST /logout` | |
| `GET /auth/oidc/login`, `GET /auth/oidc/callback` | |
| `GET /healthz`, `GET /readyz` | |

Logged-in users (`RequireUser` for GET, `RequireAdmin` for every POST):

| Route | Notes |
|---|---|
| `GET /admin` | Dashboard |
| `GET /admin/matches/new`, `POST /admin/matches` | Create |
| `GET /admin/matches/{id}/edit`, `POST /admin/matches/{id}` | Edit, only while `enable=0` |
| `POST /admin/matches/{id}/start` | form `server_id` |
| `POST /admin/matches/{id}/assign-server` | form `server_id` |
| `POST /admin/matches/{id}/{retry,reset,archive,duplicate,delete}` | |
| `GET /admin/matches/{id}/scores`, `POST /admin/matches/{id}/scores/{scoreId}` | Edit the per-side scores |
| `POST /admin/matches/{id}/command` | form `action`, `arg`. Whitelisted by `ebotcmd.ValidAction` |
| `GET /admin/matches/{id}/log` | Match log file in a sandboxed iframe |
| `GET /admin/live/{id}/rcon`, `GET /admin/live/{id}/logger`, `GET /admin/live/alive` | SSE, admin only |
| `GET /admin/{servers,teams,seasons,ads,users}` | List |
| `GET /admin/{entity}/new`, `POST /admin/{entity}` | Create |
| `GET /admin/{entity}/{id}/edit`, `POST /admin/{entity}/{id}` | Edit |
| `POST /admin/{entity}/{id}/delete` | |
| `POST /admin/{seasons,ads}/{id}/deactivate` | |
| `GET /admin/users` and everything under it | Admin only, also for GET |

API (WP-H): `/api/...`, header `X-API-Key`, keys from `EBOT_API_KEYS`.

## Bot protocol

- Start, reset and archive are plain database writes. The bot polls the database.
- Live commands go to Redis through `ebotcmd.Publisher.Send(ctx, matchID, action, arg, serverIP, authkey)`. The auth key is `matchs.config_authkey` and never leaves the server.
- `action` must pass `ebotcmd.ValidAction`. `arg` is the RCON command for `executeCommand` and the round for `goBackRounds`.
- The bot reads messages published on `EBOT_REDIS_TO_WS` as `{"scope": "...", "data": "<json string>"}`. Scopes: `match`, `rcon`, `logger`, `livemap`, `alive`. Messages whose inner data has `message: "ping"` are ignored.
- Reference: `../eBot-CSGO/websocket_server.mjs` and `../eBot-CSGO/src/eBot/Application/Application.php`.

## Match status

0 not started, 1 starting, 2 warmup knife, 3 knife, 4 end knife, 5 warmup first side, 6 first side, 7 warmup second side, 8 second side, 9 warmup overtime first, 10 overtime first, 11 warmup overtime second, 12 overtime second, 13 finished, 14 archived. Constants and labels are in `internal/store/types.go`.

## Server map ("CurrentMap")

A map named `CurrentMap` tells the bot to stay on the map the game server is already on and never run `changelevel`. It is how workshop maps are used. The bot renames it to `CurrentMap-<map>` once it knows the real map, so any name that contains `currentmap` (case-insensitive, `config.IsCurrentMap`) is a server map. `tba` means the map is chosen by veto in game. Both are always offered in the map select. On edit the stored name is also offered, so a renamed server map or a map that left `EBOT_MAPS` is kept. Show map names through the `mapLabel` template function.

## Current map (database)

`matchs.current_map` points at a row in `maps`. Create inserts the match, then exactly one `maps` row (`MatchInput.Maps` has one entry, `"tba"` when the map is chosen in game, side from `MatchInput.Side` or random), then sets `current_map` to it, all in one transaction. The bot creates the other maps of a series itself. Update renames the current map and never adds or removes maps. Lists show the current map and its score. The match view lists every map and marks the current one. `skipmap` moves it (the bot does that, the panel only sends the command).
