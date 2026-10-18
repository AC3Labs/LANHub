# PHP hub vs Go hub: measured comparison

Measured 2026-10-16 to 10-18 on the real LAN, with the same agents, the same
test files and copies of the same data. PHP hub 0.9.3.0 (the deployed
version, streaming downloads included). Go hub prototype 0.1.0.0-go
(`gohub/`). Both ran as containers on `nas` (Toshiba laptop, 2-core
Celeron, 100 Mb/s wired port).

## Results

All figures with each hub in a container on `nas`. "Host" = the container
uses `--network host`; "bridge" = Docker's default network.

| Measure | PHP hub | Go hub | Notes |
|---|---|---|---|
| Docker image | 883 MB | **21.7 MB** | Go: one static binary on `scratch` |
| Memory at idle | 91 MiB | **3.9 MiB** | `docker stats` |
| Browser download, 500 MB (bridge) | 59.8 s, peak ~55 MiB | **52.5 s**, peak **8 MiB** | both stream; checksums match |
| Browser download, 500 MB (host) | 61.4 s, peak 55 MiB | **51.1 s**, peak 8 MiB, 7.5 CPU-s | |
| Relay 1 GiB Studio Tower → Docker Server (bridge) | 193.7 s | 181.2 s | Go limited by the bridge, see below |
| Relay 1 GiB (host) | 194.0 s, **2,048 MiB written to the hub's disk**, 54 CPU-s | **101.2 s**, 3 MiB to disk, 27.4 CPU-s | checksums match |
| Search everywhere "invoice" (2 machines, warm caches) | 10.9 s, 446 hits (28 duplicates) | **6.8 s**, 418 hits | Go: parallel, nested drives de-duplicated |
| A normal page while 6 big downloads run (bridge) | **up to 17.6 s** | 0.15–0.4 s | PHP has 6 workers; each download holds one |
| Automated tests | 142 (PHP) | 30 (Go), incl. PHP-made crypto/TOTP vectors and fake-agent HTTP tests | |

### Why the relay needs host networking to show the difference

- The PHP relay downloads the whole file to the hub's disk, *then*
  uploads it: one direction at a time, each already close to the
  100 Mb/s link speed. It takes ~194 s on either network setting.
- The Go relay downloads and uploads **at the same time**. A host-level
  test (`gohub/cmd/linktest`) showed the link can do both at full speed
  (11.1 MiB/s down and 10.7 up at once). With host networking the Go relay
  reaches ~10 MiB/s (1 GiB in 101 s). On Docker's bridge network it only
  reached ~6.5 MiB/s each way: the Celeron can't do the bridge's NAT for
  full-speed traffic in both directions at once.
- Browser downloads were the same on either setting for both hubs, and
  the PHP hub showed no gain from host networking anywhere. So **there's
  no reason to change the live PHP hub's networking**; it only matters for
  a Go hub, which should run with `--network host`.

Also found: `nas` has two addresses on the same subnet, wired `.48` and
Wi-Fi `.74`. A raw test sending *to* `.74` from the Docker Server ran at
~6 MiB/s (it arrives over Wi-Fi), versus 10.8 MiB/s the other way. Worth
checking which address browsers use, but no LANHub transfer measured here
was limited by it.

## Feature parity (Go prototype)

Done: login + 2FA (authenticator, recovery codes, emailed code), dashboard
(status, drives, live speeds, add/edit/delete/share machines), Explorer
(panes, browse, hidden files, sort, list/grid, upload, download, preview,
new folder, rename, delete, drag-and-drop copy/move on one machine and
across machines, folder relays, pane search, search everywhere), transfers
panel, Activity, Agents + transfer log, Sync Rules (list/create/edit/
pause/delete; runner ported, off in tests), Users (invite, resend, admin,
delete) and the invitation set-password page. Health check, disk alerts
and ntfy are ported but off in the test containers.

Added later the same night: Profile (name, email, password, authenticator
enrolment with QR code and recovery codes, turning it off, deleting the
account), Settings (SMTP, hub address, test email), first-run Setup,
Forgot/Reset password (Laravel's `password_reset_tokens` table, 60-minute
links), and the `create-admin` / `reset-authenticator` commands
(`docker exec <container> /lanhub-hub create-admin ...`). **Every page of
the PHP hub now has a Go equivalent.**

Deliberate differences: dashboard, Agents and Activity query machines in
parallel (the PHP hub one after another); search de-duplicates nested
drives; relays stream instead of staging on disk and check that every
file's size arrived intact; the Explorer keeps its state in the browser
(no server round trip for sorting, view and hidden-file toggles).

## Bugs found in the PHP hub along the way

1. **Explorer breadcrumbs on Linux machines are relative paths**
   (`home/docker/` instead of `/home/docker/`; confirmed by calling
   `Explorer\Index::breadcrumbs()`). Clicking one asks the agent for a
   folder relative to its own working directory. One-line fix: start
   `$running` at `/` for `/`-separated machines.
2. The agent's `/api/upload` writes straight to the final name and
   silently overwrites an existing file (see `DIRECT_TRANSFER_DESIGN.md`).
3. The Windows agent installer opens port 8765 to any address on every
   network profile, Public included (see `TRANSPORT_SECURITY.md`).
4. The image ships a stale `storage/logs/laravel.log` with test entries.
5. **Previews aren't sandboxed when opened directly.** The preview route
   passes the agent's `Content-Type` through (e.g. `image/svg+xml`). Inside
   the Explorer's preview frame (`sandbox=""`) that's safe, but anyone who
   opens a preview link directly runs whatever script an SVG/HTML file
   contains with the hub's origin (session included). Someone able to put
   a file on any agent's disk could use that. The Go hub sends
   `Content-Security-Policy: sandbox` on previews; the PHP fix is the same
   one header in `PreviewController`.

## Risks of switching

- **New code path for everything that touches data.** Mitigated by the
  relay safety tests (cut-short streams, unreadable folders, no merge,
  move deletes only after success), but the PHP hub has months of real use.
- **Two front ends to keep in step** while both exist. Plan: no new
  features in either until the user decides.
- **Real-use time.** The Go hub has only been exercised by automated tests
  and scripted browser runs, not by people using it day to day.

## Decisions for the user

1. Keep evaluating the Go hub (use it side by side for real, on the data
   copy at `http://192.168.0.74:8952`), or stop here?
2. Fix the two small PHP bugs in the live hub (Linux breadcrumbs; preview
   sandbox header)?
3. Direct agent-to-agent transfers and transport security: see the two
   design documents; neither is built.
