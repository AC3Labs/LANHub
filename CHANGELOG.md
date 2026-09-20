# Changelog

All notable changes to LANHub are recorded here. Versioned as
`major.minor.patch.build`.

## [0.8.1.0] - 2026-09-20

### Fixed
- The published Docker image now enables OPcache and caches config,
  routes, and compiled views at container start, instead of PHP
  re-parsing every source file from disk on every single request. This
  had been invisible on fast hardware but made the hub noticeably
  sluggish on slower machines.
- Agent HTTP calls now fail fast (3s connect timeout) instead of hanging
  for up to 15s each. Dashboard's machine-status and network-speed
  polling calls several endpoints per machine, sequentially, on every
  page load and every `wire:poll` tick — a single asleep, firewalled, or
  otherwise unreachable machine could turn that into tens of seconds of
  hang per poll, and a few unreachable machines together could tie up
  every php-fpm worker and make the whole hub feel unusable, even though
  nothing was actually broken.

## [0.8.0.0] - 2026-09-14

### Added
- A fresh install (zero users) now shows an in-browser setup wizard at
  `/` and `/login` to create the first admin account, instead of
  requiring `php artisan lanhub:create-admin` from the command line.
  Creating the account logs you straight in (no emailed 2FA code, since
  SMTP isn't configured yet) and lands on Settings to configure outgoing
  mail next. The wizard only ever appears once — on a genuinely fresh
  install with zero users; afterward, adding people goes through the
  Users page instead, since self-registration is deliberately disabled.

### Changed
- The CLI admin-creation command still works as a scriptable
  alternative to the wizard, for anyone who wants to automate first-run
  setup instead of using the browser.

## [0.7.0.0] - 2026-09-14

### Added
- Dashboard and Machines are now one page: an accordion list per machine.
  Collapsed, each row shows name, computer name, OS (with a real
  Windows/Linux logo), LAN IP, and status; expanded, it shows full
  detail, storage, and three live speed gauges (upstream, downstream,
  average).
- The gauges are genuinely live now, not derived from file-transfer
  history — a new agent endpoint (`GET /api/netstats`, agent v1.1.0)
  reports each machine's real network-interface byte counters, and the
  hub samples it every few seconds to compute an actual current speed.

### Changed
- The Docker install no longer needs a `.env` file or a manual
  `key:generate` step — `docker compose up -d` is enough on its own.
  `APP_KEY` is generated automatically on first boot and persisted in
  the same volume as the database, so it survives restarts and image
  updates without any action from the user. Setting `APP_KEY` (or any
  other Laravel env var) directly in `docker-compose.yml`'s
  `environment:` block now just works, as expected.
- `/machines` now redirects to `/dashboard` (the pages were merged); the
  separate "Machines" nav link was removed.

## [0.6.1.0] - 2026-09-14

### Fixed
- The previous fix for the APP_KEY restart-loop only made the crash
  message clearer — it didn't stop the loop itself. Reproduced this for
  real: a fresh `docker compose up` with the last fix still restart-
  looped forever whenever the bind-mounted `.env` genuinely can't be
  written to (confirmed reproducible, not a one-host fluke). The
  container now stays up and serves a plain setup-instructions page on
  its own port instead of exiting — `docker ps` shows the real state
  (Up, not Restarting), and the actual problem is visible just by
  loading the page. Same treatment for a missing `.env` file entirely.

## [0.6.0.0] - 2026-09-14

### Fixed
- Reported by a real deployer: the container crash-looped forever on
  first deploy if `APP_KEY` was never manually generated, and nginx's
  listen port was hardcoded to 8000 regardless of any port configured
  on the deploy platform (broke on Dokploy, which routes to a specific
  container-internal port). The entrypoint now tries to generate
  `APP_KEY` itself via Laravel's own file-writing (falls back to the
  old clear-instructions-then-exit behavior only if the `.env` mount
  truly isn't writable, which does happen on some hosts), and nginx's
  port is now driven by a `PORT` environment variable (default 8000)
  instead of being baked into the image.

## [0.5.9.0] - 2026-09-14

### Fixed
- Right-clicking a file/folder in Explorer opened its context menu
  anchored to the row's kebab-menu button (CSS `absolute`), not the
  cursor — on a wide row or a pane near the screen edge, the menu could
  render off-screen entirely. It's now `fixed`-positioned at the actual
  click/right-click coordinates (clamped to stay on-screen), which also
  stops it from being clipped by a scrolled pane's overflow.

## [0.5.8.0] - 2026-09-14

### Changed
- Cross-machine search now shows real per-machine progress ("Searching
  Desktop-02…") instead of one opaque spinner for the whole run.
  `searchEverywhere()` was split into `startGlobalSearch()` (builds the
  queue) and `searchNextMachine()` (searches one machine), driven by a
  browser-side loop that awaits one real Livewire round-trip per
  machine — the label reflects an actual in-flight request to that
  machine, not a client-side timer, and a slow/unresponsive machine
  visibly takes longer instead of being hidden behind a generic spinner.

## [0.5.7.0] - 2026-09-14

### Fixed
- Cross-machine search ("Search for a file on any machine connected to
  LANHub") had no visual indicator while it was running, which can take
  a few seconds since it queries every drive on every accessible
  machine — looked like it wasn't working at all. Now shows a spinning
  icon and "Searching…" text, and disables the input until it finishes.

## [0.5.6.0] - 2026-09-14

### Fixed
- The transfer queue panel (bottom-right floating widget) had no way to
  close it — added a dismiss button. Also fixed the real reason a
  finished cross-machine transfer could sit there for up to 10 minutes
  regardless of status: Carbon 3 changed `diffInSeconds()`/etc. to
  return a signed result (negative when the argument is in the past),
  so an un-`abs()`'d `< 30` age check was always true for anything
  already in the past. Same-machine transfers had this exact bug too
  (never actually expired from the panel by age, just relied on status).

## [0.5.5.0] - 2026-09-14

### Fixed
- `docker-compose.yml` used `build: ./hub`, which only works if the
  whole repo is already checked out next to the compose file — broke
  outright on Dokploy (reported: "unable to prepare context... not
  found") and would break equally on Portainer/Coolify/any platform
  that only fetches the compose file itself. `.github/workflows/
  publish-image.yml` now publishes the hub's image to
  `ghcr.io/ac3labs/lanhub` on every push to `main` that touches `hub/`,
  and `docker-compose.yml` pulls that instead of building locally — a
  root-level `.env.example` replaces the old `hub/.env.example`
  reference for this flow.

## [0.5.4.0] - 2026-09-13

### Changed
- 2FA login codes are still 10 characters but now draw from letters,
  digits, and a handful of special characters instead of alphanumeric
  only — deliberately excludes lookalike characters (0/O, 1/I/L) and
  anything awkward to type or read in an email (backtick, quotes,
  backslash).

## [0.5.3.0] - 2026-09-13

### Fixed
- Dark mode no longer reverts to light when navigating between pages.
  `wire:navigate` swaps in fresh server-rendered HTML (which never carries
  a `dark` class, since the theme is applied client-side only) without
  re-running unchanged inline `<script>` tags, so the one-time theme
  script never fired again after the first page load. Now reapplied on
  every `livewire:navigated` event, in both the authenticated and guest
  layouts.

## [0.5.2.0] - 2026-09-13

### Fixed
- Cross-machine search now lists which machines it couldn't search and
  why, instead of silently omitting them. Found because every deployed
  agent was still running a version from before `/api/search` existed —
  each one returned a 404 that was being swallowed the same way as a
  genuinely offline machine, so a real file went unfound with no
  indication anything was wrong. All four running agents were updated to
  the current `agent.py`.

## [0.5.1.0] - 2026-09-13

### Added
- Cross-machine search in the Explorer toolbar ("Search every machine…"):
  queries every machine the user has access to (all drives on each),
  aggregates results into one flat list showing which machine each match
  is on, and clicking a result opens/reuses a pane for that machine
  navigated to the containing folder. Complements the existing per-pane
  "search this folder" box, which only ever covered one already-open
  machine. A machine that's offline or errors is skipped rather than
  failing the whole search (see 0.5.2.0 below for surfacing which ones).

### Removed
- Network Map page (`/network-map`) — the hub-and-spoke diagram didn't
  convey anything the Machines list didn't already show, since machines
  only ever connect to the hub, never to each other.

## [0.5.0.0] - 2026-09-13

Invite-only multi-user access, email-based two-factor login, and a
cleaned-up auth surface — replaces Breeze's self-registration model
entirely.

### Added
- `is_admin` flag on `User`. Only admins see the new Users page
  (`/users`) and can invite people, resend invites, grant/revoke admin,
  and delete users. Existing users are backfilled as admins on upgrade.
- Invite flow: an admin invites by name+email, which creates the user
  with an unusable password and emails a "set your password" link
  (`invite_token`, expires in 7 days — same pattern as this project's
  sibling Helpdesk codebase's staff onboarding). New public
  `/set-password/{token}` page activates the account.
- Email-based 2FA on every login: `LoginForm::authenticate()` now
  validates credentials without logging in (`Auth::validate()`, not
  `Auth::attempt()`), emails a 10-character alphanumeric code (hashed,
  10-minute expiry), and only calls `Auth::login()` after a new
  `/verify-2fa` page confirms it. Rate-limited on both login and code
  verification.
- Root route (`/`) now redirects — `dashboard` if logged in, `login`
  otherwise — replacing the static Breeze welcome page.
- Branded transactional emails (`resources/views/emails/`) for
  invitations, 2FA codes, and password resets — the latter replaces
  Laravel's default generic notification via a custom
  `ResetPasswordNotification`.

### Removed
- Self-registration (`/register`), `/confirm-password`, `/verify-email`
  and their Breeze scaffold pages/controller — this is an invite-only
  tool, and 2FA-on-every-login plus the emailed set-password link make
  separate email verification redundant.
- `resources/views/welcome.blade.php` and its orphaned navigation
  component.

### Security
- `App\Livewire\Users\Index::mount()` independently re-checks
  `is_admin` — the `admin` route middleware alone doesn't protect this
  component's individual actions (`invite`/`toggleAdmin`/`delete`),
  since those run through Livewire's separate AJAX update endpoint, not
  the original page route.

## [0.4.0.0] - 2026-09-12

Visual features — fourth and final milestone of the GitHub-release
feature set (search, transfer queue, sync rules, previews, access
control, disk-space/activity monitoring, and now the visual layer all
shipped).

### Added
- Dark mode: every color token (`paper`/`stone`/`tan` in
  `tailwind.config.js`) now resolves through a CSS custom property (see
  `resources/css/app.css`), re-pointed under a `.dark` class — no
  per-view changes needed. Toggle button in the nav, persisted to
  `localStorage`, applied before first paint to avoid a flash of the
  wrong theme.
- Thumbnails in the Explorer grid view: image files render a real
  thumbnail (via the existing preview endpoint, lazy-loaded) instead of
  the generic file icon.
- Network Map page (`/network-map`): a simple hub-and-spoke SVG diagram
  of every accessible machine, colored by its machine color, dashed/
  dimmed when offline.
- Drag-and-drop polish: a custom canvas-rendered drag ghost (filename
  pill) instead of the browser's default row screenshot, and drop-target
  highlights now tint with the destination pane's machine color instead
  of a fixed color.
- Per-machine color consistency: the Sync Rules list now shows the same
  color dot used everywhere else (Explorer panes, Transfers panel,
  Network Map) next to each rule's source/destination machine.

### Fixed
- The active nav-link's text was unreadable in dark mode (dark text on a
  light, theme-invariant tan background) — switched to a dark-tan text
  color that stays readable against that same pill in both themes.

## [0.3.0.0] - 2026-09-12

Transfer queue UI, sync rules, file preview, and search — third milestone
toward the GitHub-release feature set.

### Added
- Transfer queue panel: a floating widget (mounted globally) showing
  progress for both same-machine transfer-queue jobs (agent-side, byte
  progress) and cross-machine relays (hub-side, phase progress —
  queued/downloading/uploading/done).
- Cross-machine drag/drop now runs on the queue (`App\Jobs
  \RelayTransferJob`, backed by a new `relay_transfers` table) instead of
  blocking the Livewire request for the whole transfer. Requires a queue
  worker running — `docker-entrypoint.sh` now starts one alongside the
  existing scheduler loop.
- Sync Rules page (`/sync-rules`): one-way or mirror sync between a
  folder on one machine and a folder on another, on an interval. Diffs
  full directory trees via a new `AgentClient::listFilesRecursive()`
  (bounded, no index). Never overwrites/deletes a destination file that
  changed since it was scanned — flags the rule with a conflict instead
  of clobbering it. Runs via `sync:run-rules`, scheduled every minute.
- File preview: agent gained `GET /api/preview` — images and PDFs stream
  inline, everything else (including e.g. `.html`) is always served as
  `text/plain`, never as a type a browser would execute, since it's
  proxied through the hub's own origin. Hub adds a `PreviewController`
  route and an Explorer context-menu "Preview" action opening a
  sandboxed `<iframe>` modal.
- Cross-machine/whole-tree search: agent gained `GET /api/search`
  (bounded recursive filename search); Explorer gained a per-pane search
  box.

### Tests
- `RelayTransferTest`, `RunSyncRulesTest`, `PreviewControllerTest`,
  `ExplorerSearchTest`.

## [0.2.0.0] - 2026-09-12

Hub-side access control, HTTPS wiring, disk-space alerting, and an
activity log — second milestone toward the GitHub-release feature set.

### Added
- Per-machine access control: `machine_user` pivot with a `read_only`/
  `full` role per user, enforced in both `Machines\Index` (own the
  machine record, manage sharing) and `Explorer\Index` (browse/act on
  files). Existing users are backfilled with full access to every
  existing machine on upgrade so nobody is locked out.
- `Machines\Index` gained a "Shared with" picker per machine.
- `AgentClient` now actually honors `Machine.use_tls`, skipping cert
  verification only for a private/loopback host (never a public one).
- `App\Services\NtfyClient` — extracted from `CheckMachineHealth` so
  other alerts (disk space, and later sync/transfer failures) share one
  implementation instead of duplicating the ntfy.sh HTTP call.
- `machines:check-disk-space` (scheduled every 15 minutes): records a
  `disk_readings` row per machine and alerts via ntfy on the transition
  into <10% free space (mirrors the existing online/offline alerting
  pattern), pruning readings older than 30 days.
- Activity page (`/activity`): merges every accessible machine's own
  request log (via the agent's new `GET /api/activity`) into one feed,
  filterable by machine. No hub-side duplicate of this data is kept.

### Tests
- `MachineAccessControlTest`, `CheckDiskSpaceTest`, `ActivityPageTest`,
  plus an updated `ExplorerSmokeTest` covering the new access-control
  behavior.

## [0.1.0.0] - 2026-09-12

Agent foundations for the GitHub-release feature set (search, transfer
queue, throttling, HTTPS, activity log) — first of several planned
milestones building toward a public release.

### Added
- `agent/agent.py`:
  - `GET /api/search` — bounded recursive filename search (depth/result/
    timeout caps), no index, matches the agent's existing "always live"
    philosophy.
  - Transfer queue (`POST /api/transfers`, `GET /api/transfers`,
    `GET /api/transfers/{id}`) — background copy/move with byte-level
    progress, for anything too large to block a request on.
  - `GET /api/activity` — reads the agent's own request log back as a
    structured audit trail.
  - Chunked, optionally throttled (`throttle_kbps` in `config.json`)
    download/upload/transfer streaming.
  - HTTPS support (`cert_file`/`key_file` in `config.json`); both install
    scripts gained a `--tls`/`-Tls` flag to generate a self-signed cert.
- `docs/AGENT_API.md` updated for every new endpoint and config field.

### Fixed
- Agent error responses now return `{"message": "..."}` as documented —
  previously FastAPI's default handler returned `{"detail": "..."}`,
  which the hub's `AgentClient` silently never read, so every failure
  showed a generic fallback message instead of the real reason.
- `agent/README.md`'s Windows install step described the older Scheduled
  Task approach; corrected to reflect the current NSSM-based service
  install.

## [0.0.0.0] - 2026-09-07

Initial scaffold.

### Added
- `hub/` — Laravel 13 + Livewire 3 web app with Breeze auth, styled in a
  white/grey/light-tan palette.
- `agent/` — Python (FastAPI) agent implementing the LANHub agent API on
  both Windows and Linux, with systemd and NSSM install scripts.
- `docs/AGENT_API.md` — the hub↔agent HTTP contract.
- Machines page: register/edit/delete machines, live health-check ping,
  per-machine color tag.
- Explorer page: unlimited multi-pane file browser, breadcrumb navigation,
  list/grid view, sortable columns, hidden-file toggle, new folder, rename,
  delete, download.
- Drag-and-drop: move/copy within a machine, cross-machine relay transfer
  through the hub, and OS-file drag-in for uploads.
