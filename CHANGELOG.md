# Changelog

All notable changes to LANHub are recorded here. Versioned as
`major.minor.patch.build`.

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
