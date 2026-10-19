# Direct agent-to-agent transfers

Status: built in 0.13.0.0 (hub) and agent 1.3.0, 2026-10-19. Wire details
are in `AGENT_API.md`. Differences from the original design below:
files in a folder go one at a time (as the relay does), not two at once;
stale partial files are swept from the folder being written to when a new
fetch starts there, rather than across every root at start-up; a checksum
mismatch fails the transfer instead of falling back to the relay; and
direct transfers work over plain HTTP (TLS closes the sniffing route
described below, when it's turned on); and the pass carries `holders`, all
of the destination's addresses (reported in its `/api/health`), not one
`holder`, because a machine with wired and Wi-Fi connections can fetch from
either (seen on `nas` in testing). `LANHUB_DIRECT_TRANSFERS=false` on
the hub turns them off.

## Why

Today every cross-machine copy or move is relayed through the hub
(`RelayTransferJob`): the hub downloads the whole file to its own disk, then
uploads it to the destination. Measured 2026-10-16, 1 GiB Studio Tower →
Docker Server: **193.7 s, 5.3 MiB/s**. The hub (`nas`) has a 100 Mb/s
port that carries every byte twice, plus 1 GiB of writes to its disk.
Both endpoints have 1 Gb/s links, so a direct copy could run roughly 15–20
times faster and leave the hub idle.

Checked 2026-10-18: every agent can already reach every other agent on
port 8765 (all answer 401 without a token), so no network change is needed.

## Shape: the destination pulls

The hub tells the **destination agent (D)** to fetch a file from the
**source agent (S)**, using a short-lived **pass** that only S can verify.

Pull (rather than S pushing) keeps all writing on D, where partial files
are handled, and S only has to check a pass and stream bytes, which it
already does for `/api/download`.

```
hub ── POST /api/fetches {source_url, pass, dest, expected size} ──▶ D   (bearer T_D)
D   ── GET  /api/pass-download (X-LANHub-Pass, Range) ─────────────▶ S   (no bearer)
hub ── GET  /api/fetches/{id}  (status, bytes, sha256) ────────────▶ D   (bearer T_D)
hub ── GET  /api/hash?path=… ──────────────────────────────────────▶ S   (bearer T_S)
hub ── POST /api/fetches/{id}/commit  (if hashes match) ───────────▶ D   (bearer T_D)
hub ── POST /api/passes/{id}/retire ───────────────────────────────▶ S   (bearer T_S)
```

## The pass

Minted by the hub for one file of one transfer job:

```
fields = { v: 1, job: <relay id>, file: <n>, path: <absolute source path>,
           size: <bytes>, mtime: <RFC3339>, holder: <D's IP>,
           expires: <unix time>, nonce: <128 random bits> }
pass   = base64url(json(fields)) "." base64url(HMAC-SHA256(key = T_S, json(fields)))
```

- **Signed with S's own token (T_S).** Only the hub and S know T_S, so only
  S can check the pass, and D never learns T_S. Each agent token is
  already unique per machine.
- **Bound to one file:** S serves exactly `path`, and only if its size
  and mtime still match (the file hasn't changed since the hub listed it).
- **Expires:** `expires` = now + 15 minutes, extended by the hub while the
  job is still making progress (the hub mints a fresh pass; S accepts any
  valid unexpired pass for the same `job`/`file`).
- **Resumable:** D may reconnect with `Range` using the same pass after a
  dropped connection.
- **One active stream:** S keeps the nonces it has seen in memory and
  refuses a second concurrent stream for the same nonce (409). A stream that
  ends releases it, so resume works.
- **Retired** by the hub once D's checksum is confirmed (below). S then
  refuses it even if unexpired. S also forgets retired/expired nonces after
  expiry, so its memory use is bounded.
- **`holder` IP check:** S refuses a request whose source address isn't
  `holder`. This is **defence in depth only**: on a LAN, addresses can be
  spoofed or shared (NAT, a second NIC on `nas`), so it must never be the
  only check.

## What is and isn't authenticated

Be explicit about this, because it is the weak point:

- **The receiving agent is not cryptographically authenticated to S.**
  S can prove the pass came from the hub, but not that the caller is D.
  Anyone who obtains a valid pass, by reading plain HTTP between the hub
  and D or between D and S, can download that one file until the pass
  expires or is retired. The limits above (one file, short life, one
  stream, IP check) bound the damage; they don't remove it. TLS (see
  `TRANSPORT_SECURITY.md`) closes the sniffing route.
- **Checksum confirmation is authenticated by the existing bearer tokens,
  per agent:** the hub gets D's sha256 of the received bytes from D (bearer
  T_D) and S's sha256 of the source from S (bearer T_S), and compares them
  itself. Neither agent vouches for the other. Over plain HTTP a network
  attacker could alter either answer, the same exposure as every other agent
  call today.
- No agent ever holds another agent's token.

## Partial files are never shown as complete

On D:

1. Write to `<dest folder>/.lanhub-partial-<job>-<file>` (same folder, so the
   final rename is atomic on the same filesystem).
2. Compute sha256 while writing.
3. Report `status: received` with size and sha256. Don't touch the real
   name yet.
4. On `commit`: refuse if the destination name now exists (409, matching
   today's "already exists" rule), otherwise rename atomically.
5. On `abort`, expiry, or a hash mismatch: delete the partial file.
6. `/api/list` and `/api/search` hide `.lanhub-partial-*` entries, and D
   deletes stale partials (older than 24 h) at start-up.

The hub only deletes the source of a **move** after every file's commit has
succeeded, the same rule as `RelayTransferJob` today.

Note: today's `/api/upload` writes straight to the final name and
**silently overwrites** an existing file (`os.Create`). That's a current
bug worth fixing separately; this design doesn't depend on it.

## Folders

The hub walks the source folder (as `RelayTransferJob` does now), creates
folders on D, and runs one fetch per file, with at most 2 files in flight
per job. Empty folders, depth limit (64) and "unreadable folder fails the
whole job" behave as today.

## Fallbacks and compatibility

- Agents advertise support in `/api/health` (`"features": ["fetch"]`).
  If either side lacks it, or D can't reach S (connect error within 3 s),
  the hub falls back to today's relay through itself.
- Old hubs never call the new endpoints, so new agents stay compatible.
- `throttle_kbps` on S still applies to pass downloads.

## Expected result

Studio Tower → Docker Server, 1 GiB: limited by the slower disk or the
1 Gb/s link, so roughly 10–15 s instead of 194 s, with zero bytes through
`nas`. To be measured before any decision.

## Open questions for the user

1. Is the residual risk above acceptable on this LAN **before** TLS is
   turned on, or should direct transfers require TLS?
2. Should the Windows firewall rule be narrowed (see
   `TRANSPORT_SECURITY.md`)? Direct transfers need agents to accept
   connections from each other, not just from the hub.
