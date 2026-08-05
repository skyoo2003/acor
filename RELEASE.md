# Releasing ACOR

This document describes how maintainers cut a release. It reflects the actual
automation in the repo, not an aspirational process.

## Overview

| Concern         | Tool                                 | Config                           |
| --------------- | ------------------------------------ | -------------------------------- |
| Versioning      | SemVer git tags `vX.Y.Z`             | —                                |
| Changelog       | [changie](https://changie.dev)       | `.changie.yaml`, `changes/`      |
| Build & publish | [GoReleaser](https://goreleaser.com) | `.goreleaser.yaml`               |
| Release trigger | GitHub Actions on tag push           | `.github/workflows/release.yaml` |

**changie** is the source of truth for release notes. Its per-version file
(`changes/vX.Y.Z.md`) is what GoReleaser publishes as the GitHub release body.

## Prerequisites

- [`changie`](https://changie.dev/guide/installation/)
- [`goreleaser`](https://goreleaser.com/install/) (only for local dry-runs)
- Push access to tags on `skyoo2003/acor`

The release workflow itself needs no manual secrets — it uses the built-in
`GITHUB_TOKEN` for both the GitHub release and pushing images to GHCR.

Run every command below from the repo root. The `v` prefix is mandatory and
must match across the board: `changie batch vX.Y.Z` writes `changes/vX.Y.Z.md`,
and the tag build guards on exactly `changes/<tag>.md` — so the changie version
and the git tag have to be the identical `vX.Y.Z` string.

## During development (every contributor)

Each change that should appear in the changelog gets a fragment:

```sh
changie new
```

This prompts for a kind (`Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`,
`Security`, `Documentation`), a body line, and the issue number. It writes a
YAML fragment under `changes/unreleased/`. Commit it with your change.

## Cutting a release (maintainer)

1. **Confirm `main` is green** and holds everything you want to ship.

2. **Pick the version** (`vX.Y.Z`, SemVer). Base the bump on the kinds of the
   unreleased fragments in `changes/unreleased/` — the major-version call is
   always yours.

3. **Batch the fragments** into a version file:

   ```sh
   changie batch vX.Y.Z
   ```

   This consumes `changes/unreleased/*` and writes `changes/vX.Y.Z.md`.

4. **Regenerate the changelog:**

   ```sh
   changie merge
   ```

   Rebuilds `CHANGELOG.md` from `changes/header.tpl.md` + all version files.

5. **Commit and merge to `main` via PR:**

   ```sh
   git switch -c release/vX.Y.Z
   git add CHANGELOG.md changes/
   git commit -m "chore: release vX.Y.Z"
   git push -u origin release/vX.Y.Z
   ```

   Open the PR, get it green, and merge.

6. **Tag and push** once merged. Sync `main` first so the tag lands on the
   merge commit, not your local release branch:

   ```sh
   git switch main && git pull
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

   The tag push triggers `.github/workflows/release.yaml`.

## What the tag build does

`release.yaml` runs on any `v*` tag except `v1.4.1`
(see [Retracted versions](#retracted-versions)) and:

1. **Guards** that `changes/<tag>.md` exists — fails fast if you forgot
   `changie batch` / `changie merge`.
2. Runs GoReleaser with `--release-notes changes/<tag>.md`, which produces:
   - **Binaries** for darwin / linux / windows across `386`, `amd64`, `arm`,
     `arm64` (see `.goreleaser.yaml` for excluded combos), packaged as `.tar.gz`
     (`.zip` on Windows), each bundling `LICENSE`, `README.md`, `CHANGELOG.md`,
     `CODE_OF_CONDUCT.md`.
   - A **`CHECKSUMS`** file (sha256).
   - **Docker images** pushed to `ghcr.io/skyoo2003/acor`, tagged
     `<tag>-alpine`, `vMAJOR.MINOR-alpine`, `vMAJOR-alpine`, and `latest-alpine`
     (built from `Dockerfile.goreleaser`).
   - A **GitHub release** named `vX.Y.Z` with the changie notes as its body.

Release mode is `replace`, so re-running the tag build overwrites the existing
release's assets rather than appending.

The `v*` glob also matches pre-release tags: pushing `vX.Y.Z-rc.1` runs the
same pipeline, so it needs its own `changes/vX.Y.Z-rc.1.md` and would move
`latest-alpine` onto the RC. This flow assumes final `vX.Y.Z` tags — don't push
a pre-release tag unless you mean to.

## Verify

- GitHub release exists with the expected notes and artifacts.
- `docker pull ghcr.io/skyoo2003/acor:vX.Y.Z-alpine` works.
- `latest-alpine` points at the new version.

## Rollback

To redo a botched release: fix the fragments/changelog, delete the tag locally
and remotely (`git push origin :vX.Y.Z`), then re-tag and push. The build
recreates the release in place (`replace` mode, above).

Watch the Docker tags: deleting the git tag does **not** remove images already
in GHCR — they stay until a build overwrites them. And `latest-alpine` (plus
`vMAJOR-alpine` / `vMAJOR.MINOR-alpine`) always points at whichever tag built
*last*, so re-running an older tag's build silently drags `latest-alpine` back
to that older version. When redoing an older release, re-tag the newest one last.

## Retracted versions

`v1.0.0`-`v1.4.0` were published from tags that no longer exist here. Deleting a
tag does not unpublish a module version — `proxy.golang.org` caches it forever —
so they stayed resolvable, and `go get github.com/skyoo2003/acor` picked
`v1.4.0` over the v0.x line. The whole range is retracted in the root `go.mod`:

```go
retract [v1.0.0, v1.4.1]
```

`v1.4.1` carries that block and retracts itself, which is what makes `@latest`
fall back to the highest v0.x. It is cut from `main`, so it publishes the current
v0.x tree under a v1 number — harmless because it is retracted, but it is not an
empty tag.

Two rules when editing this:

- **Keep the block on `main`.** The go command reads retractions only from the
  highest published version's `go.mod`, so a future v1 tag without it would
  un-retract the whole line.
- **Only the first comment line reaches users.** The go command truncates a
  retraction rationale at the first newline, so keep that line a complete,
  actionable sentence.

`release.yaml` skips its job for exactly this tag
(`if: ${{ github.ref_name != 'v1.4.1' }}`), so GoReleaser never publishes
`v1-alpine` or moves `latest-alpine` onto retracted code. The match is exact
rather than a `v1.` prefix so that a real **v1.5.0** releases with no edit here,
and any other stray v1 tag fails loudly on the changie guard instead of skipping
in silence.

### Publishing the retraction tag

**Not reversible** — the proxy caches whatever you push. A `v1.4.1` tag cut from
a commit *without* the `retract` block becomes the highest published version and
carries no retractions, so `go get` resolves to it: permanently worse than the
state you are fixing.

1. Merge the `retract` block to `main` first.
2. Tag the merge commit, never a release branch:

   ```sh
   git switch main && git pull
   grep -A1 'retract' go.mod        # the block must be there
   git tag v1.4.1
   git push origin v1.4.1
   ```

3. Expect a skipped workflow run, no GitHub release, and no `changes/v1.4.1.md`.
4. Verify from a scratch module outside this repo:

   ```sh
   curl -s https://proxy.golang.org/github.com/skyoo2003/acor/@v/v1.4.1.info
   go list -m github.com/skyoo2003/acor@latest     # want the highest v0.x
   go list -m -versions github.com/skyoo2003/acor  # v1.x must be gone
   ```

   The `curl` is needed because the proxy has to fetch the retraction-carrying
   version before retractions apply. Do not add `-retracted`: it lists retracted
   versions alongside the rest, so the output is the same before and after.

A real v1 starts at **v1.5.0**, which `[v1.0.0, v1.4.1]` already excludes, so
leave both the range and the workflow condition alone.
