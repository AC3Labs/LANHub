# Transport security (decision document)

Status: for the user to decide, 2026-10-18. Separate from the hub-language
question; applies to the PHP hub and any Go hub alike.

## Today

| Item | State |
|---|---|
| Hub ↔ agent protocol | Plain HTTP on port 8765. TLS exists but is opt-in (`install-linux.sh --tls`, `use_tls` on the machine) and is **off on every machine**. |
| Agent authentication | One static bearer token per agent, sent on **every** request in clear text. Anyone who can see one request can replay that token forever (until it's rotated). |
| Token check | `authMiddleware` compares with `!=`, not constant-time. Minor on a LAN, trivial to fix (`subtle.ConstantTimeCompare`). |
| TLS verification when on | The hub sets `verify => false` for private addresses, so even with TLS a machine on the LAN could impersonate an agent (no pinning). |
| Agent scope | `allowed_roots` is supported but empty on both agents checked, so a token gives full access to every drive the agent's account can read and write. |
| Windows firewall | The installer adds "LANHub Agent": inbound TCP 8765, **remote address Any, profile Any** (including *Public*). A laptop agent on hotel or café Wi-Fi accepts connections from that network. Confirmed on Studio Tower. |
| Browser ↔ hub | Plain HTTP on `http://192.168.0.74:8000`. Logins and session cookies cross the LAN in clear text. |

Everything is on one home subnet (192.168.0.x) with no NAT between
machines, which keeps the exposure to devices on the home network, as long
as no agent machine travels.

## Options

**A. TLS with pinned self-signed certificates (recommended)**
- Each agent generates its own key pair at install (all platforms, not only
  `--tls` on Linux) and serves HTTPS only.
- The hub records the certificate's SHA-256 fingerprint when the machine is
  added (trust on first use, shown to the user to confirm) and **pins** it:
  a different certificate is refused, never silently accepted.
- Stops sniffing of tokens and file contents, and stops agent
  impersonation. Makes direct transfers' passes un-sniffable.
- Cost: an agent release, a hub change (store and check the fingerprint),
  a one-time re-registration of each machine. CPU cost of TLS at 1 Gb/s
  is small on all current machines; `nas` (Celeron) is the slowest, and
  should be measured.

**B. Narrow the firewall (cheap, do regardless)**
- Windows rule: profile **Private/Domain only**, remote address
  **LocalSubnet** (or the hub and other agents' addresses).
- Linux agents: optional `ufw` rule with the same scope in
  `install-linux.sh`.
- Doesn't encrypt anything; stops a travelling laptop from exposing its
  agent on foreign networks.

**C. Request signing instead of a static token**
- The hub signs each request (HMAC over method, path, query, timestamp,
  nonce) instead of sending the token; agents reject old timestamps and
  reused nonces.
- Stops replay of captured requests but not reading of file contents. Less
  value than A for similar work; only worth it if TLS is ruled out.

**D. Overlay network (WireGuard, Tailscale)**
- Strong, but adds a dependency and setup on every machine. Overkill for a
  single home LAN unless remote access is wanted later.

**E. HTTPS for the browser ↔ hub**
- Put the hub behind the existing Nginx Proxy Manager with a certificate,
  or have the hub serve TLS itself. Separate from A–D but the same
  reasoning: logins currently cross the LAN in clear text.

## Recommendation

1. **B now** (small installer change, no protocol change).
2. **A** as the next agent release, together with constant-time token
   comparison and an `allowed_roots` prompt in the installers.
3. **E** when the hub decision (PHP or Go) is made, since it affects how
   the hub is deployed.
4. Direct transfers (`DIRECT_TRANSFER_DESIGN.md`) can ship before A, with
   the residual risk documented there, or wait for A. The user decides.

None of this is implemented. All of it needs the user's go-ahead.
