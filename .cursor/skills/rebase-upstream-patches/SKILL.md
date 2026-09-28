---
name: rebase-upstream-patches
description: >-
  Rebases the local patches branch of this argocd-image-updater fork onto a
  newer upstream release tag and updates patches. Use when the user asks to
  apply patches to a new upstream tag, move patches onto a new version, or
  update the fork after an argoproj-labs/argocd-image-updater release.
---

# Rebase patches onto an upstream tag

This fork keeps custom commits on branch `patches`. Upstream remote is `original-repo` (`https://github.com/argoproj-labs/argocd-image-updater.git`). Do not commit patch work onto `master`.

## Ask for the tag

If the user did not name the upstream tag, fetch tags and ask. Do not pick one yourself.

```bash
git fetch original-repo --tags
git tag -l 'v*' --sort=-v:refname | head -20
```

Show that list and the current base (below). Wait for the user to choose the target tag.

## Current base

The current base is the `original-repo` tag that is an ancestor of `patches` with the smallest number of commits to `patches`.

```bash
git fetch original-repo --tags
git rev-parse --verify patches
```

Resolve the base with `git merge-base --is-ancestor <tag> patches` and `git rev-list --count <tag>..patches`. If no tag is an ancestor, stop and say so.

## Rebase

Stop if the worktree is dirty.

```bash
git rebase --onto <new-tag> <current-base> patches
```

`<new-tag>` must be a tag fetched from `original-repo`. This replays only the commits that are on `patches` and not in `<current-base>`.

On conflict, stop. Do not skip commits and do not abort unless the user asks. Report the conflicting files.

After a clean rebase, show `git log --oneline <new-tag>..patches` and run:

```bash
go test ./pkg/argocd/ -count=1 -timeout 180s -run 'TestPushWithRetry|TestRetryDelay|TestValidateGitPushRetry'
go test ./ext/git/ -count=1 -timeout 180s -run 'TestPullRebase|TestPush'
```

## Do not push or tag

Leave `patches` local. Push only if the user explicitly asks. The rebase rewrites commits, so the push is `git push --force-with-lease origin patches`. Never force-push `master`.

A git tag such as `v1.4.0-fix1` on the new `patches` tip is what starts the image build and GitHub release (`.github/workflows/patches.yaml`). Create that tag only if the user asks, and do not reuse an existing tag name.
