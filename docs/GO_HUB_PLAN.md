# Go hub: migration plan and feature inventory

Status: Phase 3 of the evaluation agreed with the user (2026-10-16). The Go
hub is built **alongside** the PHP hub, in a separate container on `nas`,
against a **copy** of the data, with background jobs off. Nothing replaces
the PHP hub unless the comparison shows the Go hub is equivalent or better
and the user decides to switch. Nothing is pushed to GitHub.

## Goals and non-goals

Goals, in order:

1. Same data, same rules: reads the PHP hub's SQLite database as-is
   (no schema changes), uses the same `APP_KEY` to decrypt agent tokens and
   2FA secrets, same access model (per-machine `read_only`/`full`, admins).
2. Same agents, unchanged (agent API v1).
3. Measurably better where the PHP hub is weak: browser downloads,
   cross-machine relays (stream through memory, no disk staging), parallel
   "search everywhere", idle footprint, image size.
4. Close enough in look and flow that comparing them is fair: same
   Tailwind CSS bundle, same page structure.

Non-goals for the evaluation: new features, agent changes, direct
agent-to-agent transfers (designed separately), TLS changes.

## Architecture

- Single static binary, Go standard library `net/http` (1.22+ routing),
  `html/template`, `database/sql` with **modernc.org/sqlite** (pure Go, no
  cgo), `golang.org/x/crypto/bcrypt`. Docker image `FROM scratch` plus CA
  certificates and timezone data.
- Server-rendered pages. Interactivity (Explorer panes, transfers panel,
  dashboard refresh) uses small vanilla JavaScript calling JSON endpoints
  on the hub, instead of Livewire.
- Background work (health checks, disk alerts, sync rules, relay jobs) runs
  as goroutines in the same process. A `-scheduler=false` flag (the
  default in test containers) turns off everything periodic; relays still
  run because a user starts them.
- Sessions: its own HttpOnly cookie, `SameSite=Lax`, server-side session
  rows in its own table (`go_sessions`), CSRF token per session checked on
  every POST. It doesn't share sessions with the PHP hub.

## Compatibility details that must be exact

| Item | PHP hub | Go hub |
|---|---|---|
| Passwords | bcrypt `$2y$` (cost 12) | `x/crypto/bcrypt` reads `$2y$`; new hashes written as `$2y$` so the PHP hub can still read them |
| Encrypted columns (`machines.agent_token`, `users.two_factor_secret`, `users.two_factor_recovery_codes`, `settings.value`) | Laravel `Crypt`: base64 JSON `{iv, value, mac, tag}`, AES-256-CBC, HMAC-SHA256 over `iv.value` | Same format, both ways, with the key from `APP_KEY` (`base64:`) |
| TOTP | Google2FA, 30 s, 6 digits, window ±1, replay guard via `two_factor_last_used_step` | Same, same column |
| Recovery codes | `encrypted:array`, single use | Same |
| Email login code | 10 characters, bcrypt-hashed, 10 min | Same columns |
| Dates | `Y-m-d H:i:s` UTC text | Same |
| Machine access | `machine_user.role`, no implicit admin access | Same |

## Feature inventory

Size is PHP + Blade lines. Risk: how likely the Go version differs in a way
users notice.

| # | Feature | PHP location | Size | Risk | In prototype |
|---|---|---|---|---|---|
| 1 | Login, logout, remember me | LoginForm, pages/auth/login | 175 | Low | Yes |
| 2 | 2FA: authenticator code, recovery code, emailed code | verify-2fa, User, Authenticator | 410 | Medium (exact TOTP and replay rules) | Yes (app + recovery; email code if mail set) |
| 3 | Dashboard: machine cards, status, drives, network, sorting, add/edit/delete machine (admin) | Dashboard\Index | 575 | Medium (largest UI) | Yes |
| 4 | Explorer: panes, navigate, up, drives, hidden files | Explorer\Index | 1076 | High (most interaction) | Yes |
| 5 | Explorer: upload, mkdir, rename, delete | Explorer\Index | (in 4) | Medium | Yes |
| 6 | Explorer: download, preview (streaming) | Download/PreviewController, AgentStream | 154 | Low | Yes |
| 7 | Explorer: same-machine copy/move (agent queue) | Explorer\Index + agent transfers | (in 4) | Medium | Yes |
| 8 | Cross-machine relay, file and folder, copy and move | RelayTransferJob | 181 | High (data safety) | Yes, streamed |
| 9 | Transfers panel (relays + agent jobs) | Transfers\Panel | 140 | Low | Yes |
| 10 | Search in a pane, search everywhere | Explorer\Index | (in 4) | Low | Yes, parallel |
| 11 | Activity page (agents' activity logs) | Activity\Index | 103 | Low | Yes |
| 12 | Agents page, agent log viewer | Agents\Index, Log | 290 | Low | Yes |
| 13 | Sync rules: list, create, edit, delete, run now | SyncRules\Index | 272 | Medium | UI yes; runner off in tests |
| 14 | Sync rule runner (one-way, mirror, conflicts) | RunSyncRules | 165 | High (data safety) | Ported, not run on real data |
| 15 | Health check every minute + ntfy/email alert | CheckMachineHealth, NtfyClient | 120 | Low | Ported, off in tests |
| 16 | Disk space check every 15 min + alert | CheckDiskSpace | 78 | Low | Ported, off in tests |
| 17 | Users: list, invite, roles, per-machine access (admin) | Users\Index | 298 | Medium | Yes |
| 18 | Invitation / set password / forgot / reset password | pages/auth/* | 244 | Medium | Yes (added 2026-10-18) |
| 19 | Settings: SMTP (admin) | Settings\Index | 246 | Low | Yes (added 2026-10-18) |
| 20 | Setup wizard (first admin) | Setup\Index | 86+ | Low | Yes (added 2026-10-18) |
| 21 | Profile: name, email, password, 2FA enrol/disable, delete account | profile/* | 476 | Medium | Yes (added 2026-10-18) |
| 22 | CLI: create-admin, reset-authenticator | Console\Commands | 128 | Low | Yes (added 2026-10-18) |

## Risks and how they're handled

1. **Data safety in relays and sync.** The Go relay keeps the PHP rules: no
   merge into an existing folder, an unreadable subfolder fails the whole
   job, and a move deletes the source only after everything arrived. Tested
   against throwaway folders only (the same test folders as the baselines).
2. **Two hubs, one set of agents.** Both hubs can run agent operations at
   once. Agents are stateless apart from their transfer queue, so this is
   safe, but test actions must use the baseline test folders only.
3. **Encryption compatibility.** Covered by unit tests that decrypt values
   produced by the PHP hub, and by the PHP hub decrypting values produced by
   Go, before anything is written.
4. **UI parity.** The Go hub reuses the PHP hub's built CSS, so only
   markup and behaviour can differ. The comparison notes any gaps.
5. **Scope creep.** Items marked "Later" stay out until the comparison is
   done and the user decides.

## How the comparison is run

Same machines, same test files, same copy of the data, measured the same
way as the PHP baselines in `LANHub.md`:

| Measure | PHP hub (measured) | Go hub |
|---|---|---|
| Docker image size | 882 MB | to measure |
| Idle memory / CPU | 63 MiB / ~1.5% | to measure |
| Browser download 500 MB | 59.8 s, memory flat ~55 MiB (0.9.3.0) | to measure |
| Relay 1 GiB Studio Tower → Docker Server | 193.7 s, 1 GiB written to the hub's disk | to measure |
| Search everywhere, "invoice" | ~44 s (sequential) | to measure |
| Concurrent large downloads | limited by 6 PHP workers | to measure |
| Test coverage | 142 PHP tests | Go tests per feature |
