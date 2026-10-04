---
name: nulas-worktree
description: Run a Nulas task in an isolated Git worktree. Use when a request starts with `wt:`; create a worktree and branch, do and commit the task there, rebase onto main, fast-forward main, then remove the worktree and delete its branch.
---

# Nulas worktree tasks

A request that starts with `wt:` is a directive: the rest of the message is the task and must run in an isolated worktree. Finish with the work landed on `main` and no worktree or branch left behind.

## Rules

- Use `.worktrees/<slug>` in the primary checkout as the working tree and `wt/<slug>` as the branch. Derive a short lowercase kebab-case slug from the task; `wt: fix tray icon on Windows` becomes `wt/fix-tray-icon-on-windows`.
- One task, one worktree. Never remove, rebase or reuse a worktree or branch created for another task.
- Never touch the primary checkout's uncommitted changes: do not switch its branch, stash, or discard files.
- Never force (`git worktree remove --force`, `git branch -D`) past a failure unless the user explicitly asked to discard the work.
- Never push, and never rewrite commits that are already on `main`.
- `.worktrees/` is ignored by Git; keep it ignored.

## 1. Create

Run from the primary checkout, which is normally on `main`:

```bash
git rev-parse --abbrev-ref HEAD          # expect: main
git worktree add -b wt/<slug> .worktrees/<slug> main
```

- The new branch starts at the current local `main`. Fetch only if the task explicitly depends on remote changes.
- If branch `wt/<slug>` or path `.worktrees/<slug>` already exists, pick a different slug or report; do not delete the existing one.
- Do not create the worktree from a branch other than `main`.

## 2. Work in the worktree

- Address the worktree by path (`git -C .worktrees/<slug> ...`) or `cd` into it; keep the primary checkout on `main` and untouched.
- A fresh worktree lacks ignored assets: `web/node_modules/`, `.output/`, `.vinxi/`, `.runtime/` and `bin/`. Run `cd web && pnpm install --frozen-lockfile` inside the worktree when frontend checks are needed.
- Run the checks the task requires (see the `AGENTS.md` Commands section) and record unavailable verification honestly.
- Commit inside the worktree with conventional English messages (`feat:`, `fix:`, `docs:`), splitting large tasks into coherent commits.
- Finish with a clean worktree: `git -C .worktrees/<slug> status --porcelain` prints nothing.
- Stop long-running dev servers before integrating; their ports may collide with the primary checkout.

## 3. Land and clean up

```bash
# Replay the task commits on the current main, inside the worktree
git -C .worktrees/<slug> rebase main

# Fast-forward main to the rebased commits, from the primary checkout
git merge --ff-only wt/<slug>

# Remove the worktree, then the branch
git worktree remove .worktrees/<slug>
git branch -d wt/<slug>

# Verify the end state
git worktree list
git branch --list 'wt/*'
```

- Rebase before removing the worktree so conflicts are resolved where the branch is checked out. Re-run the affected checks after resolving. If the rebase cannot be completed, keep the worktree and branch and report.
- `git merge --ff-only` refuses if `main` has diverged or if local changes would be overwritten. Do not stash or discard the user's changes; report and stop.
- `git worktree remove` refuses when the worktree has modified or untracked files; ignored files such as `node_modules/` do not block it. Inspect leftovers, commit what belongs, delete disposable files, and use `--force` only if the user explicitly discards the work.
- `git branch -d` refuses when the commits are not merged, which means the fast-forward did not happen. Never use `-D` to hide that.
- If the primary checkout is not on `main`, land the branch with `git branch -f main wt/<slug>` instead of the merge, and only while `main` is not checked out in any worktree; otherwise report.
- If a worktree directory was deleted by hand, run `git worktree prune` before creating a new one.
- A task that produces no commits (investigation only) still ends with the worktree and branch removed; nothing is landed.

## 4. Report

Report the landed commit hashes and messages in a table as the `AGENTS.md` Completion and Git section requires, the checks that ran, and any verification that was unavailable. Confirm that `main` contains the commits and that no `wt/<slug>` worktree or branch remains.
