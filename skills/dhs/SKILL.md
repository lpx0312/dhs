---
name: dhs
description: Find container images on Docker Hub, browse an image's tags, filter by CPU architecture (amd64/arm64/arm), and generate ready-to-paste `docker pull` commands — all through the `dhs` CLI. Use this whenever the user wants to discover or select a Docker image: searching by keyword, looking up an image's tags or versions, finding images for a specific architecture, or getting pull commands. Trigger on requests like "find a redis image", "what tags does nginx have", "I need an arm64 postgres image", "give me the docker pull command for alpine", "search docker hub for X" — even when Docker Hub is not named explicitly. This skill is only for *discovering* images; do not use it for building, pushing, or running containers.
---

# dhs — Docker Hub Image Discovery

`dhs` is a small CLI that talks to the Docker Hub API. It has exactly two jobs:

- `dhs search <keyword>` — find image *repositories* by keyword
- `dhs tags <image>` — list an image's *tags*, filter by architecture, and emit `docker pull` commands

Most real requests are a two-step hop: **search** to pick the right repository, then **tags** to pick a version and get the pull command. Run `dhs` via Bash and read the formatted tables it prints.

Assume `dhs` is already on `PATH` and callable directly. (If a run reports "command not found", the user can build it from the project root with `go build -o dhs .` and add it to `PATH` — but only do this if asked.)

## Proxy setup — required before any dhs call

Docker Hub is only reachable through a proxy on this network, so **every `dhs` command must carry the proxy**. The proxy lives in the `DOCKERHUB_PROXY` environment variable (default value `http://192.168.0.225:7897`). The `dhs` binary does not read `DOCKERHUB_PROXY` directly — the skill reads it and forwards it via the `--proxy` flag.

Before the first `dhs` command in a task, check that `DOCKERHUB_PROXY` is set, and set it if it is not:

```bash
# 1. Check whether it is already set
echo "${DOCKERHUB_PROXY:-<not set>}"

# 2. If the line above printed <not set>, set it for this session:
export DOCKERHUB_PROXY=http://192.168.0.225:7897
```

If you had to set it, tell the user, and suggest making it permanent by adding that `export` line to their shell profile (`~/.bashrc`, `~/.zshrc`, or the PowerShell `$PROFILE`). Once it is in the profile, every new shell — and every Bash call here — picks it up automatically.

Environment variables do not carry between separate Bash calls, so when in doubt pass the proxy with a built-in default. This form works whether or not the guard above just ran, in a single self-contained call:

```bash
dhs --proxy "${DOCKERHUB_PROXY:-http://192.168.0.225:7897}" search nginx
```

The examples below use the shorter `--proxy "$DOCKERHUB_PROXY"` form, which is fine once the variable is confirmed set (ideally in the profile so it persists).

## Step 1 — Find repositories: `dhs search`

```bash
dhs --proxy "$DOCKERHUB_PROXY" search <keyword>            # top 25 results (default)
dhs --proxy "$DOCKERHUB_PROXY" search <keyword> -n 10      # fewer results
```

Output columns: `NAME`, `STARS`, `PULLS`, `OFFICIAL` (✓ official / ✗ community), `DESCRIPTION`.

Reading the results:
- A `✓` in OFFICIAL means it's a Docker-maintained image (e.g. `nginx`, `redis`) — usually the safest default.
- `user/repo` names (e.g. `bitnami/nginx`) are community images; mention who maintains them when it matters.
- Show the table, then add a one-line recommendation when the user's intent is clear.

## Step 2 — Inspect tags: `dhs tags`

```bash
dhs --proxy "$DOCKERHUB_PROXY" tags nginx                       # newest 10 tags by default
dhs --proxy "$DOCKERHUB_PROXY" tags nginx --filter alpine       # only tags whose name contains "alpine"
dhs --proxy "$DOCKERHUB_PROXY" tags nginx --all                 # every tag (can be hundreds)
dhs --proxy "$DOCKERHUB_PROXY" tags nginx --sort size           # sort by size | name | date (default date)
dhs --proxy "$DOCKERHUB_PROXY" tags user/repo                   # non-official image → use user/repo form
```

Output columns: `Tag`, `Status`, `Size(MB)`, `Architecture`, `LastUpdated`.

A bare name like `nginx` is shorthand for the official `library/nginx`. For community images pass the full `user/repo`. Note that the search API does not return an update time, so `search` shows a description column instead — only `tags` has `LastUpdated`.

## Step 3 — Architecture filtering + pull commands

When the user cares about CPU architecture (common for ARM servers, Apple Silicon, Raspberry Pi), combine `--arch` with `--gen-pull`:

```bash
dhs --proxy "$DOCKERHUB_PROXY" tags nginx --arch amd64,arm64 --gen-pull
```

- `--arch` keeps only tags that ship the requested architecture(s); comma-separated, OR semantics (a tag matching *any* listed arch is kept). Filtering looks at the architecture field, so `--arch arm64` still matches `arm64/v8`.
- `--gen-pull` prints `docker pull` commands after the table. **It only takes effect when `--arch` is also given** — without `--arch`, dhs prints a notice and skips it, because without a fixed architecture the generated commands would be ambiguous.
- amd64 → plain `image:tag` (the default platform on x86 hosts).
- arm64 → `--platform=linux/arm64 image:tag`.

Export to Markdown when the user wants to save or share the result:

```bash
dhs --proxy "$DOCKERHUB_PROXY" tags nginx --arch amd64,arm64 --gen-pull -o nginx-tags.md
```

## Mapping common requests to commands

**Example 1 — "I need a redis image"**
Search, then recommend the official one.
```bash
dhs --proxy "$DOCKERHUB_PROXY" search redis
```

**Example 2 — "What versions/tags does postgres have?"**
```bash
dhs --proxy "$DOCKERHUB_PROXY" tags postgres
```
Add `--filter alpine` for a slim variant, `--all` to see everything.

**Example 3 — "Give me pull commands for nginx for amd64 and arm64"**
```bash
dhs --proxy "$DOCKERHUB_PROXY" tags nginx --arch amd64,arm64 --gen-pull
```

**Example 4 — "Find a lightweight python image and get its pull command for my ARM server"**
```bash
dhs --proxy "$DOCKERHUB_PROXY" search python                       # pick the official `python`
dhs --proxy "$DOCKERHUB_PROXY" tags python --filter slim --arch arm64 --gen-pull
```

## If a command still fails after setting the proxy

With `DOCKERHUB_PROXY` set, the connection should succeed. If a command still times out or is refused, the proxy itself is likely wrong or down — confirm the address with the user rather than guessing a different one, and check that the proxy host (`192.168.0.225:7897`) is reachable.

## Quick flag reference

Global (placed before the subcommand): `--proxy URL` / `-P`, `--config PATH`, `--no-color`.

| subcommand | useful flags |
|---|---|
| `search <keyword>` | `-n, --limit N` (default 25) |
| `tags <image>` | `-n, --limit N` (10), `--all` / `-a`, `--sort name\|size\|date`, `--order asc\|desc`, `--filter SUBSTR`, `--arch a,b`, `--gen-pull`, `-o FILE` |
