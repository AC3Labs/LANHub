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
  and two-factor login with an authenticator app (Google Authenticator,
  Authy, 1Password…) or an emailed code.
- Disk-space monitoring and online/offline alerting via a free ntfy.sh
  push notification.
- An activity log, a per-machine agents page (live health, transfer
  throughput, and a transfer log), and dark mode.
- Optional HTTPS between the hub and each agent (self-signed, generated
  by the install scripts).

See `docs/AGENT_API.md` for the full hub↔agent wire contract.

## Architecture

- **`gohub/`** — the hub: a single Go program (about 15 MB image) that
  serves the web app you open in a browser. Runs in Docker.
- **`gohub/cmd/lanhub-media`** — LANHub Media, an optional second container
  (ffmpeg) that lets the Explorer preview nearly any video.
- **`hub/`** — the previous hub (Laravel + Livewire), kept for reference
  and for upgrading old installs. The Go hub reads its database unchanged.
- **`agent/`** — a small Go service, one per machine you want to browse.
  Compiles to a single static binary (no runtime to install) and runs
  natively (not in Docker) so it has direct access to that machine's
  real filesystem — see `agent/README.md`.

## Requirements

- Docker on an x86-64 (Intel/AMD) server, for the hub.
- Nothing extra on the machines you want to browse — the agent is a
  single static binary (Windows and Linux, `agent/dist/`).
- Optional: an SMTP account for outgoing mail (inviting other people,
  password resets, emailed login codes). Any provider works. You can use
  LANHub on your own without it by logging in with an authenticator app.

## Setup

### 1. Run the hub

You only need one file, `docker-compose.yml`, and you don't have to edit it.
You don't need to clone this repo, create a `.env` file, or generate any
keys. It starts two containers: the hub, and LANHub Media for video
previews.

**Plain Docker** (any Linux server, x86-64):

```
curl -O https://raw.githubusercontent.com/AC3Labs/LANHub/main/docker-compose.yml
docker compose up -d
```

Then open `http://<your-server's-IP>:8949` in a browser.

**Dokploy, Coolify, Portainer and similar:** create a new Docker Compose
app and paste in the contents of
[`docker-compose.yml`](docker-compose.yml) unchanged. Deploy it, then open
`http://<your-server's-IP>:8949`. To use a domain instead, add it in
your platform's domain settings, pointing at service `hub`, port `8000`.

The first time you open it you'll see a setup screen. Create your account
there and you're logged in as the admin. Every login after that asks for a
one-time code, so it takes you straight to setting up an **authenticator
app**: scan a QR code with Google Authenticator, Microsoft Authenticator,
Authy, 1Password or similar, and type in the code it shows. Save the
recovery codes it gives you in case you lose your phone. Later, set up
outgoing email under **Settings** so you can invite other people.

Everything else is automatic. The database and encryption key are
created on first start and kept in the `hub_data` volume, so they survive
restarts and updates. LANHub works out its own address from however you
reach it: an IP, a hostname, a different port, or a domain with HTTPS
through a reverse proxy. You don't have to configure it anywhere.

(The setup screen only appears on a brand-new install. To add more people
later, use the **Users** page; people can't sign themselves up. To script
this step instead, run `docker compose exec hub /lanhub-hub create-admin -name NAME -email
EMAIL -password PASSWORD`.)

**If it doesn't start:**

- *"port is already allocated"* means another program on your server is
  already using port 8949. In `docker-compose.yml`, change the left-hand
  number under `ports:`, e.g. `"8950:8000"`. Deploy again and browse to
  that port instead.
- *"no matching manifest"* means your server isn't x86-64 (for example a
  Raspberry Pi or other ARM device). Only x86-64 is published right now.
- Anything else: run `docker compose logs hub`. The hub explains what's
  wrong in plain language, either in that log or on the page itself.

### 2. Install an agent on each machine you want to browse

See `agent/README.md`. Short version: add the machine on the hub's
Dashboard first (**Add Machine**; it gives you a token), then on that
machine, from a copy of this repo's `agent/` folder:

- **Linux:** run `sudo install/install-linux.sh` from your normal account
  (not a root shell, or the agent will run as root). Put the token in
  `/opt/lanhub-agent/config.json`, then `sudo systemctl start lanhub-agent`.
- **Windows:** right-click Start, choose **Terminal (Admin)**, and run
  `powershell -ExecutionPolicy Bypass -File install\install-windows.ps1`.
  Put the token in `C:\LANHub-Agent\config.json`, then
  `Restart-Service LANHubAgent`.

On Windows 11 with **Smart App Control** turned on, Windows may refuse to
start the agent because it isn't code-signed yet ("An Application Control
policy has blocked this file"). There's no way to allow a single program
while Smart App Control is on.

### Upgrading from 0.9

0.9 was a different hub (PHP), and its compose file keeps the database in a
volume called `hub_db`. 0.11 reads that database and encryption key
unchanged, so nobody has to set anything up again; they just need copying
into the new volume once. In the folder with your old `docker-compose.yml`:

```
docker compose down                      # stops the old hub; your data stays in its volume
curl -O https://raw.githubusercontent.com/AC3Labs/LANHub/main/docker-compose.yml
docker compose create                    # makes the new containers and volumes, starts nothing
docker run --rm -v "${PWD##*/}_hub_db:/old:ro" -v "${PWD##*/}_hub_data:/new" alpine sh -c \
  'cp /old/database.sqlite /new/ && { [ ! -f /old/.app_key ] || cp /old/.app_key /new/app.key; } && chown -R 65532:65532 /new'
docker compose up -d
```

If your old compose file set `APP_KEY` itself, set the same `APP_KEY` in the
new one. (`docker volume ls` shows the volume names if your project isn't
named after its folder.) Log in as before. The old volume is left alone,
so going back is just restoring the old compose file. Agents from 0.9 keep
working; updating them to 1.2.0 makes seeking in big videos faster.

## Security notes

- This is built for a trusted LAN, not the public internet. Don't expose
  the hub or any agent directly to the internet without putting real
  authentication/network controls in front of them.
- The hub talks to agents over plain HTTP, with a token, unless you
  install the agent with `--tls`/`-Tls` (a self-signed certificate; there's
  no public CA involved). Fine on a home network you trust; turn it on if
  you don't. See `agent/README.md`.
- The browser talks to the hub over plain HTTP too, unless you put it
  behind a reverse proxy with HTTPS.
- Two-factor login is required for every user on every login; there is no
  "remember this device" bypass. Each user picks an authenticator app or
  emailed codes. If someone loses their phone and their recovery codes, an
  admin can run `docker compose exec hub /lanhub-hub reset-authenticator
  their@email` to switch them back to emailed
  codes.

## Development

```
cd gohub
go test ./...
LANHUB_DB=./dev.sqlite go run .   # first start creates the database and app.key beside it
```

Styles are Tailwind, built from the `hub/` config into
`gohub/web/static/app.css`.

## License

Free to use, modify, and redistribute (including commercially), as long as
original-author credit and the license notice are kept — see `LICENSE`.
