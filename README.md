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
- An activity log and dark mode.
- Optional HTTPS between the hub and each agent (self-signed, generated
  by the install scripts).

See `docs/AGENT_API.md` for the full hub↔agent wire contract.

## Architecture

- **`hub/`** — a Laravel + Livewire web app. This is what you open in a
  browser. Runs in Docker.
- **`agent/`** — a small Python (FastAPI) service, one per machine you
  want to browse. Runs natively (not in Docker) so it has direct access
  to that machine's real filesystem — see `agent/README.md`.

## Requirements

- Docker (for the hub).
- Nothing extra on the machines you want to browse — the agent is a
  single static binary (Windows and Linux, `agent/dist/`).
- An SMTP account for outgoing mail (invites, login codes, password
  resets) — any provider works. Without one configured, mail just gets
  written to the server log instead of actually being delivered.

## Setup

### 1. Run the hub

Only `docker-compose.yml` and a `.env` file are needed — it pulls a
prebuilt image (published automatically from this repo), so this works
on any docker-compose-driven platform (Dokploy, Portainer, Coolify,
plain `docker compose up`) without cloning the repo at all. If you do
have the repo checked out already:

```
cp .env.example .env
```

Edit `.env` — at minimum set `APP_URL` to how you'll reach the hub (e.g.
`http://192.168.1.10:8000`). Then generate an encryption key (this only
prints one, it doesn't need to write anything, which matters on hosts
where the container can't write back to a bind-mounted `.env`):

```
docker compose run --rm hub php artisan key:generate --show
```

Paste the output into `.env` as `APP_KEY=base64:...`, then:

```
docker compose up -d
```

First start runs migrations automatically. Create your first account:

```
docker compose exec -u www-data hub php artisan lanhub:create-admin
```

(This only works once — on a fresh install with zero users. To add more
people afterward, log in and use the Users page instead; self-registration
is deliberately disabled.)

Log in at the URL you set in `APP_URL`, then go to **Settings** and
configure SMTP so invites, login codes, and password resets actually get
delivered.

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
