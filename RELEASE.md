# Releasing ACOR

This document describes how maintainers cut a release. It reflects the actual
automation in the repo, not an aspirational process.

## Overview

| Concern         | Tool                                 | Config                                                                  |
| --------------- | ------------------------------------ | ----------------------------------------------------------------------- |
| Versioning      | SemVer git tags `vX.Y.Z`             | —                                                                       |
| Changelog       | [changie](https://changie.dev)       | `.changie.yaml`, `changes/`                                             |
| Build & publish | [GoReleaser](https://goreleaser.com) | `.goreleaser.yaml`                                                      |
| Release trigger | GitHub Actions on tag push           | `.github/workflows/release.yaml`                                        |
| Draft preview   | Release Drafter on merge to `main`   | `.github/workflows/release-drafter.yaml`, `.github/release-drafter.yml` |

Two independent things produce release notes:

- **changie** is the source of truth. Its per-version file (`changes/vX.Y.Z.md`)
  is what GoReleaser publishes as the GitHub release body.
- **Release Drafter** keeps a *draft* GitHub release assembled from merged-PR
  labels — a preview to help pick the next bump (step 2). It publishes nothing
  and is not the notes that ship.

## Prerequisites

- [`changie`](https://changie.dev/guide/installation/)
- [`goreleaser`](https://goreleaser.com/install/) (only for local dry-runs)
- Push access to tags on `skyoo2003/acor`

The release workflow itself needs no manual secrets — it uses the built-in
`GITHUB_TOKEN` for both the GitHub release and pushing images to GHCR.

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

2. **Pick the version** (`vX.Y.Z`, SemVer). Release Drafter's current draft
   suggests a bump from merged PR labels — treat it as a hint, not a rule. It
   has no `major` resolver and maps breaking changes to a *minor* bump, so the
   major-version call is always yours.

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

`release.yaml` runs on any `v*` tag and:

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
