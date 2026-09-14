# LANHub

A private, self-hosted file explorer for your own LAN — browse, move, copy,
rename, and search across every computer you own from one browser tab, with
drag-and-drop between machines. Built to replace unreliable SMB shares.

A small "hub" web app (this repo's `hub/`) talks to a lightweight "agent"
(`agent/`) running on each machine you want to browse. Works the same way
across Windows and Linux.

## Features

- Multi-pane file browser: any number of machines open side by side, with
  drag-and-drop move/copy (same-machine or cross-machine), list/grid view,
  sortable columns, hidden-file toggle, per-machine color tags.
- Cross-machine and whole-tree search.
- Inline preview for images, PDFs, and text files.
- A transfer queue with progress for anything too large to wait on.
- Scheduled sync rules (one-way or mirror) between two machines, with
  conflict detection — never silently overwrites a file that changed
  since it was last scanned.
- Invite-only multi-user access with per-machine read-only/full sharing,
  and email-based two-factor login.
- Disk-space monitoring and online/offline alerting via a free ntfy.sh
  push notification.
- An activity log, a per-machine agents page (live health, transfer
  throughput, and a transfer log), and dark mode.
- Optional HTTPS between the hub and each agent (self-signed, generated
  by the install scripts).

See `docs/AGENT_API.md` for the full hub↔agent wire contract.

## Architecture

- **`hub/`** — a Laravel + Livewire web app. This is what you open in a
  browser. Runs in Docker.
- **`agent/`** — a small Go service, one per machine you want to browse.
  Compiles to a single static binary (no runtime to install) and runs
  natively (not in Docker) so it has direct access to that machine's
  real filesystem — see `agent/README.md`.

## Requirements

- Docker (for the hub).
- Nothing extra on the machines you want to browse — the agent is a
  single static binary (Windows and Linux, `agent/dist/`).
- An SMTP account for outgoing mail (invites, login codes, password
  resets) — any provider works. Without one configured, mail just gets
  written to the server log instead of actually being delivered.

## Setup

### 1. Run the hub

Just `docker-compose.yml` is needed — no `.env` file, no key-generation
step. It pulls a prebuilt image (published automatically from this
repo), so this works on any docker-compose-driven platform (Dokploy,
Portainer, Coolify, plain `docker compose up`) without cloning the repo
at all:

```
curl -O https://raw.githubusercontent.com/ac3labs/lanhub/main/docker-compose.yml
```

Open it and change `APP_URL` to how you'll reach the hub (e.g.
`http://192.168.1.10:8000`), then:

```
docker compose up -d
```

That's it — first start runs migrations and generates its own encryption
key automatically (saved in the `hub_db` volume, so it survives restarts
and updates without you doing anything). Everything else in the file is
optional, for when you want it: pin your own `APP_KEY`, change the
container-internal port for a platform that needs it, etc. — see the
comments in `docker-compose.yml`.

Open the URL you set in `APP_URL` — a fresh install (zero users) shows a
setup wizard automatically, right there in the browser. Create your
account and you're straight in as an admin; it walks you into
**Settings** next to configure SMTP so invites, login codes, and
password resets actually get delivered (you can skip that and come back
to it later).

(The wizard only appears once — on a fresh install with zero users. To
add more people afterward, use the Users page instead; self-registration
is deliberately disabled. If you'd rather script this step, `docker
compose exec -u www-data hub php artisan lanhub:create-admin` does the
same thing from the command line.)

**Using a different port?** If you just want the hub reachable on a
different *host* port, change the left-hand side of `docker-compose.yml`'s
`ports:` mapping (e.g. `"8949:8000"`) — nothing else needs to change. Some
platforms (Dokploy and similar) instead route to a specific
container-internal port; for that, set the `PORT` environment variable in
`docker-compose.yml` and update the container side (right-hand side) of
the `ports:` mapping to match.

### 2. Install an agent on each machine you want to browse

See `agent/README.md`. Short version: register the machine from the hub's
Machines page first (it generates a token), then on that machine:

```
cd agent
cp config.example.json config.json   # paste the generated token in
sudo install/install-linux.sh        # or install/install-windows.ps1 as Administrator
```

## Security notes

- This is built for a trusted LAN, not the public internet. Don't expose
  the hub or any agent directly to the internet without putting real
  authentication/network controls in front of them.
- Agent-to-hub HTTPS uses self-signed certificates (there's no public CA
  involved) — see the `--tls`/`-Tls` install flags in `agent/README.md`.
- Two-factor login is required for every user on every login; there is no
  "remember this device" bypass.

## Development

```
cd hub
composer install
npm install && npm run build   # or `npm run dev` while working on frontend
cp .env.example .env
php artisan key:generate
php artisan migrate
php artisan test
```

## License

Free to use, modify, and redistribute (including commercially), as long as
original-author credit and the license notice are kept — see `LICENSE`.
