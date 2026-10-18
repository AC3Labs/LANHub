# Search specification

This document describes how search works in LANHub today (hub 0.9.3.0,
agent 1.1.0), what has been measured, where it falls short, and the
behaviour any replacement hub (including the Go hub being evaluated) must
match or deliberately improve on.

There is no index anywhere in LANHub. Every search walks the filesystem
live on the agent.

## 1. Agent: `GET /api/search`

Parameters: `path` (folder to start in), `query` (required), `max_depth`
(default 8), `max_results` (default 200), `timeout` in seconds
(default 10). A value that doesn't parse as a number falls back to its
default. Values are not clamped.

Response `200`:

```json
{ "path": "<resolved start folder>", "query": "...", "entries": [Entry, ...], "truncated": false }
```

`Entry` has the same shape as `/api/list` entries: `name`, `path`
(absolute), `type` (`file` or `dir`), `size` (files only), `modified`
(RFC 3339, UTC), `hidden`.

Errors: `400` when `query` is empty; `404` when `path` doesn't exist or
isn't a folder; `403` when `path` is outside the agent's `allowed_roots`.
Like every agent endpoint, it needs the agent's bearer token.

### Matching

- Case-insensitive **substring** match on the entry's **name** only, not
  its path or contents.
- The query is literal: `*`, `?` and quotes have no special meaning.
- Files and folders both match. A matching folder is reported and also
  walked into.

### Walk order and limits

- Depth-first, in the order `os.ReadDir` returns (sorted by name).
- `max_depth = N`: a folder at depth N still has its children checked for
  matches, but nothing below them is walked. Matches can therefore be
  reported at depth N+1.
- `max_results`: the walk stops as soon as that many matches are found and
  sets `truncated: true`. Reaching the limit on the very last match also
  reports `truncated: true`.
- `timeout`: checked before each folder is read. When the time is up the
  walk stops and sets `truncated: true`. Results found so far are returned.
- Symlinked folders are never walked into (no loops). A symlink whose name
  matches is reported; `type` reflects what it points to.
- A folder that can't be read is skipped **silently**: it doesn't set
  `truncated`.

These rules are pinned by `agent/search_test.go`.

## 2. Hub: search in one Explorer pane

`Explorer\Index::search($paneId)`:

- Query is trimmed; an empty query clears the pane's results.
- Needs at least read-only access to the machine (`canAccess`), otherwise 403.
- Starts in the pane's current folder, or `C:\` (Windows) / `/` (others)
  when the pane is at the machine's drive list.
- Calls the agent with default limits (the hub never sends
  `max_depth`, `max_results` or `timeout`).
- `truncated` shows the pane message "Showing partial results — the
  search hit its limit."; an agent error shows its message instead.

## 3. Hub: "Search everywhere"

`startGlobalSearch()` then `searchNextMachine()` called repeatedly by the
browser:

- Machines: every machine the user can access, ordered by name. One
  Livewire round trip per machine, so the progress text names the machine
  really being searched.
- For each machine: `GET /api/drives`, then one search per drive starting
  at the drive root, **one drive after another**.
- A machine that is offline or errors is listed under failures (once per
  machine) and the search carries on.
- Any truncated drive marks the whole search partial.
- Results carry machine id, name and colour plus the entry fields.
  Opening a result opens (or reuses) a pane on that machine at the
  containing folder.

Pinned by `hub/tests/Feature/ExplorerSearchTest.php`.

## 4. Measured baseline (2026-10-18, live agents, via a copy of the hub)

| Machine (drives) | "log" (common) | "invoice" (rare) | "zzqqnotfound" (none) |
|---|---|---|---|
| NAS (`/`, `/home/nas`) | 6.4 s, 266 hits, partial | 10.1 s, 0, partial | 10.1 s, 0, partial |
| Studio Tower (`C:` `D:` `E:` `F:`) | 3.9 s, 794 hits, partial | 20.7 s, 388, partial | 12.4 s, 0, partial |
| Docker Server (`/`, `/home/docker`) | 2.0 s, 284, partial | 10.1 s, 56, partial | 4.0 s, 0 |
| ASUS Studio (offline) | 3.3 s to fail | | |

"Search everywhere" for "invoice" therefore takes about **44 s**
(10.1 + 20.7 + 3.3 + 10.1), because machines and drives are searched
one after another. Done in parallel it would take about as long as the
slowest single drive (~10 s).

## 5. Known issues

1. **Sequential everywhere-search.** Total time is the sum of every drive
   on every machine, and an offline machine costs its 3 s connect timeout
   in the middle of the run.
2. **"No results" is not trustworthy.** Whole-disk searches on Linux hit
   the 10 s cap (`/` walks `/proc`, `/sys`, `/dev`, `/run`, Docker's
   overlay folders), so an empty, "partial" answer usually means "ran out
   of time", not "not there".
3. **Duplicates.** On Linux the drive list includes `/` and also
   `/home/<user>`, which is inside `/`. Matches under the home folder
   appear twice.
4. **Depth-first bias.** With a 200-result cap, a common word fills up
   from the first folders alphabetically; shallow matches in later folders
   are never reached.
5. **Silent gaps.** Unreadable folders aren't reported, so a search can
   miss things without saying "partial".
6. **No limit clamping on the agent.** A caller can pass a huge `timeout`
   or `max_results`. Only the hub calls it, with a valid token, so this is
   about robustness, not an open hole.
7. **Holds a PHP worker.** Each Livewire search round trip holds one of
   the hub's 6 PHP-FPM workers for up to the whole machine's search time
   (Studio Tower: about 21 s).

## 6. Required behaviour for a replacement hub

A replacement hub must keep, unchanged:

- the agent API above (agents stay as they are);
- access rules: only machines the user can access, read-only access is
  enough to search;
- the result fields and "open containing folder" behaviour;
- per-machine failure reporting that doesn't abort the search;
- a visible "partial results" state whenever any agent says `truncated`.

It may improve, and should, in this order:

1. **Search machines in parallel** (and drives within a machine), with a
   per-machine concurrency cap of 2, and stream each machine's results to
   the browser as they arrive.
2. **Remove duplicates** by dropping a drive whose path is inside another
   drive being searched on the same machine.
3. **Skip pseudo filesystems** on Linux (`/proc`, `/sys`, `/dev`, `/run`,
   `/var/lib/docker`) when searching from `/`. This needs an agent
   change (new optional `exclude` parameter, ignored by old agents), so it
   is listed for a later agent release.
4. **Breadth-first walk** so shallow matches come first. Also an agent
   change.
5. **Report skipped folders** (`skipped: <count>`) so "partial" covers
   unreadable folders too. Also an agent change.
6. **Clamp parameters on the agent** (`max_results` ≤ 1000, `timeout` ≤ 60,
   `max_depth` ≤ 32).

Items 1 and 2 are hub-only and part of the Go hub evaluation. Items 3–6
need a new agent version and are deliberately out of scope until the hub
decision is made.
