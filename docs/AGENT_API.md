# LANHub Agent API Contract

The hub (Laravel, runs on the Ubuntu box) talks to a small agent process running
on every machine you register. Every agent — Windows or Linux — implements this
same HTTP contract so the hub's code never branches on OS.

## Transport

- Plain HTTP on the LAN by default (port `8765`), optionally HTTPS if the
  machine has a cert (`use_tls` on the Machine record).
- Every request must carry `Authorization: Bearer <agent_token>`. The token is
  generated when you register the machine in the hub and pasted into the
  agent's config file on that machine. Requests without a valid token get
  `401`. The one exception is `/api/pass-download` (see "Direct transfers"),
  which takes a hub-signed pass instead.
- All request/response bodies are JSON except `/api/download` (raw bytes) and
  `/api/upload` (multipart form).
- Paths are always **absolute, native paths** for that machine's OS
  (`C:\Users\jsmith\Documents` or `/home/jsmith/projects`). The hub never
  tries to normalize across OSes — it just displays what the agent reports.

## Endpoints

### `GET /api/health`
Returns liveness + identity info, polled by the hub to show online/offline status.
```json
{ "hostname": "desktop-01", "os": "windows", "version": "1.3.0", "features": ["range", "fetch"] }
```
`features` lists what the agent can do beyond the 1.1.0 API: `range` (1.2.0,
Range requests on downloads and previews) and `fetch` (1.3.0, direct
transfers). Older agents send no `features`. 1.3.0 also sends
`addresses`, the machine's own IP addresses (not loopback), which the hub
puts in a direct-transfer pass as the addresses the fetch may come from.

### `GET /api/drives`
Returns the root volumes to seed the file tree.
```json
{ "drives": [
  { "path": "C:\\", "label": "Local Disk (C:)", "free": 120384984, "total": 512000000 },
  { "path": "D:\\", "label": "Data (D:)", "free": 900000, "total": 2000000 }
]}
```

### `GET /api/netstats`
Cumulative bytes sent/received since the network interface(s) came up
(everything but loopback) — not a rate. The hub samples this repeatedly
and derives a live speed from the delta between two samples itself,
the same way `nload`/Task Manager's network graph works.
```json
{ "bytes_sent": 128374651, "bytes_recv": 998172345 }
```

### `GET /api/list?path=...`
Lists one directory (non-recursive).
```json
{ "path": "C:\\Users\\jsmith", "entries": [
  { "name": "Documents", "path": "C:\\Users\\jsmith\\Documents", "type": "dir", "size": null, "modified": "2026-09-01T12:00:00Z", "hidden": false },
  { "name": "notes.txt", "path": "C:\\Users\\jsmith\\notes.txt", "type": "file", "size": 4821, "modified": "2026-09-05T08:30:00Z", "hidden": false }
]}
```

### `GET /api/download?path=...`
Streams the raw file bytes with a `Content-Disposition` header.

### `POST /api/upload` (multipart)
Fields: `path` (destination directory), `file` (the uploaded blob). Writes the
file into that directory using its original filename.

### `POST /api/mkdir`
Body: `{ "path": "C:\\Users\\jsmith\\New Folder" }`

### `POST /api/rename`
Body: `{ "path": "...", "new_name": "renamed.txt" }` — renames in place, same directory.

### `POST /api/move`
Body: `{ "source": "...", "destination": "..." }` — destination is the full new path.

### `POST /api/copy`
Body: `{ "source": "...", "destination": "..." }` — recursive for directories.

### `POST /api/delete`
Body: `{ "path": "...", "recursive": true }` — recursive required to delete non-empty directories.

### `GET /api/search?path=&query=&max_depth=&max_results=&timeout=`
Recursive, bounded search under `path` for names containing `query`
(case-insensitive substring match). No index exists anywhere in this
system — this walks the filesystem live on every call. `max_depth`
(default 8), `max_results` (default 200) and `timeout` in seconds
(default 10) all cap the walk; hitting any of them returns whatever was
found so far with `truncated: true` rather than an error.
```json
{ "path": "C:\\Users\\jsmith", "query": "invoice", "entries": [ /* same shape as /api/list entries */ ], "truncated": false }
```

### Transfer queue
For anything sizeable, `/api/copy`/`/api/move` above block the request
for the full duration with no progress reporting. The queue below runs
the same operation on a background thread instead:

- `POST /api/transfers` — body `{ "source": "...", "destination": "...", "kind": "copy" | "move" }` (default `"copy"`). Returns the created job immediately (`status: "pending"`).
- `GET /api/transfers/{id}` — poll one job's status/progress.
- `GET /api/transfers` — the 50 most recent jobs, newest first.

```json
{ "id": "a1b2c3...", "kind": "copy", "source": "...", "destination": "...", "status": "running", "bytes_done": 40000000, "bytes_total": 120000000, "error": null, "created_at": "2026-09-12T10:00:00Z" }
```
`status` is one of `pending`, `running`, `done`, `error` (with `error`
set to a message when it fails). This queue is in-process and in-memory
only — jobs do not survive an agent restart.

### `GET /api/activity?limit=200`
Returns the agent's own request log (most recent first) as the audit
trail — every request is logged except the once-a-minute `/api/health`
poll. Not persisted anywhere else; this is a read of the same rotating
log file the agent writes to disk.
```json
{ "entries": [ { "time": "...", "method": "GET", "path": "/api/list?path=C:\\Users", "status": 200, "client": "192.168.1.10" } ] }
```

### `GET /api/preview?path=`
Inline preview of one file — images (`jpeg`/`png`/`gif`/`webp`/`bmp`/`svg`)
and PDFs stream their raw bytes with the real content-type and
`Content-Disposition: inline`; anything guessed as text (or with no
guessable type) is served as **`text/plain`, always** — even a `.html` or
`.svg` file's markup is returned as inert text, truncated at 512KB, never
as a type a browser would execute — since this is proxied through the
hub's own origin and an arbitrary file must never be able to run script
there. Every response also carries `X-Content-Type-Options: nosniff`.
Anything else (executables, archives, unrecognized binary types) returns
`415`.

### Bandwidth throttling
`config.json`'s `throttle_kbps` (default `0` = unlimited) caps
`/api/download`, `/api/upload`, and every transfer-queue job to roughly
that many KB/s by sleeping between fixed-size chunks. It's a soft,
best-effort limit, not a precise traffic shaper.

## Error format

Any non-2xx response body:
```json
{ "message": "Permission denied writing to C:\\Windows\\System32" }
```

## HTTPS

Set `use_tls` on the Machine record in the hub, and set `cert_file`/
`key_file` in the agent's `config.json` to a certificate/key pair (the
install scripts can generate a self-signed one with `--tls` /`-Tls`).
Since these are self-signed, the hub does not attempt to validate them
against a public CA — see `app/Services/AgentClient.php` for how that's
scoped.

## Cross-machine transfers

When both agents support `fetch` (1.3.0 and later), the destination agent
pulls each file straight from the source agent (below). Otherwise, or if a
direct transfer can't start, the hub streams it: download from A, upload to
B, without writing it to the hub's disk.

## Direct transfers (agent 1.3.0, feature `fetch`)

Design and threat model: `docs/DIRECT_TRANSFER_DESIGN.md`. In short, the hub
signs a **pass** for one file with the *source* agent's token, gives it to
the destination, and the destination downloads with it. No agent ever holds
another agent's token.

Pass: `base64url(json) "." base64url(HMAC-SHA256(key = source token, json))`,
json = `{v: 1, job, file, path, size, mtime, holders, expires, nonce}`.

### Source side
- `GET /api/pass-download` with header `X-LANHub-Pass: <pass>` and no bearer
  token. Serves exactly `path` (Range supported) if the signature checks, the
  pass hasn't expired or been retired, the caller's address is one of
  `holders` (when given), and the file's size and mtime still match. `401` bad/expired pass,
  `403` retired or not from a holder address, `409` file changed or the pass is already
  streaming.
- `GET /api/hash?path=` → `{ "path", "size", "sha256" }`.
- `POST /api/passes/retire` `{ "job": 12, "file": 1 }`: refuse every pass for
  that job/file from now on.

### Destination side
- `POST /api/fetches` `{ "source_url", "pass", "destination", "size",
  "insecure" }` → a fetch `{ "id", "destination", "size", "status", "bytes",
  "sha256", "error" }`. `destination` is the full path of the new file;
  `409` if it already exists. The file is written to
  `.lanhub-partial-<id>` in the same folder (hidden from `/api/list` and
  `/api/search`), hashed as it arrives, and resumed with Range after a
  dropped connection. `insecure` skips certificate checks for a source on a
  private address with a self-signed certificate.
- `GET /api/fetches/{id}`: status is `fetching`, `received` (complete, with
  `sha256`), `error`, `committed` or `aborted`.
- `POST /api/fetches/{id}/pass` `{ "pass" }`: a fresh pass for reconnects
  during a long fetch.
- `POST /api/fetches/{id}/commit`: renames the partial file to the real
  name. `409` if the fetch isn't `received` or the name now exists.
- `POST /api/fetches/{id}/abort`: stops the fetch and deletes the partial
  file.

The hub commits only after the destination's `sha256` matches the source's
`/api/hash`, and deletes the source of a move only after every file is
committed.
