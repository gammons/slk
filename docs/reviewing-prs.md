# Reviewing pull requests

How slk PRs get reviewed and labelled. Written so an agent can run a
review pass from this file alone; read `AGENTS.md` first for the
codebase rules every review checks against.

## Ground rules

- **Never post before the maintainer signs off.** Draft every comment
  and label, present the findings, and wait. Posting, labelling,
  closing, merging and tagging are the maintainer's decisions.
- **Re-check state before acting.** PRs move between turns: authors
  push, other PRs merge, `main` changes underneath. Before reviewing,
  and again immediately before posting, confirm each PR's head SHA is
  the one you reviewed. If it moved, re-review the new commits.
- **Every claim in a comment is one you verified yourself.** Reproduce
  it, quote the exact output, cite `file:line` at the reviewed SHA. A
  subagent's finding is a lead, not a fact: re-run the decisive ones
  before they go in a comment. If you can't reproduce it, cut it.
- **Don't touch the maintainer's working tree.** Leave the main
  checkout, `.worktrees/*`, and existing local branches alone. Review
  in throwaway worktrees and delete them (and their branches) after.

## Which PRs need a pass

```sh
gh pr list --state open --limit 50 \
  --json number,title,author,isDraft,labels,updatedAt
```

A PR needs review when:

- it has **no label** and isn't the maintainer's own (`gammons`), or
- it has **commits newer than the maintainer's last review comment**.
  Comments alone don't count; an author saying "fixed" with nothing
  pushed gets a short "the commit hasn't reached the PR" reply.

`updatedAt` is unreliable: merges elsewhere and label edits bump it.
Compare commit dates against the last `gammons` comment instead.

Also check:

- **Held CI.** First-time contributors' runs wait for approval:
  `gh api "repos/gammons/slk/actions/runs?status=action_required"`.
  Say in the comment when results come from local runs only.
- **Superseded PRs.** If a merged PR or a better open one fixes the
  same thing, flag the older one for the maintainer to close. Don't
  close it yourself.

## Setting up

One worktree per PR, outside the repo:

```sh
git fetch -q origin "+refs/pull/N/head:refs/heads/rv-N"
git worktree add -q /tmp/slkreview/rvN rv-N
# ... review ...
git worktree remove /tmp/slkreview/rvN --force && git branch -D rv-N
```

PRs are independent, so review several in parallel with one subagent
each, every one given its own pre-made worktree. Subagents start with
no context: paste the PR description, the relevant `AGENTS.md` rules,
the prior review's findings (for a re-review), and tell them explicitly
not to post to GitHub or touch anything outside their worktree.

## The review

Do every step; the order matters less than completeness.

1. **Run the gates, and test the PR's own claims.** These are the same
   gates the author is asked to run (see `AGENTS.md`):

   ```sh
   go build ./... && go vet ./... && gofmt -l .
   golangci-lint run
   go test ./... -race -count=1
   ```

   Then check every claim in the description: "fails on main", "covers
   click", "no behaviour change", etc.

2. **Merge current `main` and run the gates again.**
   `git -c user.email=r@r -c user.name=r merge origin/main --no-edit`.
   A PR that's green on its own branch can conflict, fail to compile, or
   be made obsolete by something that landed since. Always check for
   that last one; it has happened.

3. **Reproduce the bug on `main`, then confirm the fix.** Drive it
   through the real `a.Update` path. `runKeyCases` and direct helper
   calls bypass the reducer chain (see `AGENTS.md`), so a test written
   that way doesn't prove the user-facing path works.

4. **Mutation-test the new code.** This is the highest-value step.
   Break each guard, condition and branch the PR adds, one at a time:
   delete it, invert it, off-by-one it. Run the tests and record caught
   or survived. A guard that can be deleted with every test still green
   is untested, whatever the coverage looks like. Also check each new
   test fails on the pre-fix code. Revert every mutation
   (`git checkout -- .`) before the next.

5. **Hunt for real bugs.** Edge cases (empty, one item, boundaries,
   before first render, resize), failure paths (errors swallowed,
   cleanup skipped, partial writes), concurrency, cross-platform
   (`GOOS=darwin`/`windows go build ./...`; nix's sandbox has no
   `/bin/sh`), and side effects of reusing an existing function for a
   new purpose. Write throwaway probe tests to prove what you suspect.

6. **Check it against `AGENTS.md`.** The `internal/ui` I/O boundary,
   new behaviour in a `reducer_*.go` rather than `Update`, reuse of
   listed helpers instead of a new copy, new reusable helpers added to
   the table, the `messages`/`thread` lockstep, `main.go` scope
   (`main_scope_test.go`), and refactors that change behaviour.

7. **Check docs and goldens.** Wiki pages and design docs match the
   code. A re-blessed golden must be current (`-update` produces no
   further diff), and every changed line must be explained.

## Labels

| label | meaning |
|---|---|
| `ready to merge` | No blockers. Optional suggestions only. |
| `needs minor changes` | Approved in principle; a small, specific fix is required first, typically a missing test for a guard. |
| `changes requested` | Blocking: red CI, a reproduced bug, or a fix of the same bug class the PR claims to fix. |

- **Drafts get a comment, never a label.**
- On re-review, swap the label (`--add-label X --remove-label Y`)
  rather than stacking them.
- Red CI is blocking even when the production code is right. Say so,
  and that the fix is in the test.

## Writing the comment

Shape (look at recent `gammons` comments for the voice):

1. **Lead with what's genuinely good**, specifically. Thank the author;
   welcome first-time contributors.
2. **A verification table**: each check, its result, and whether it was
   run on the branch, merged with `main`, or locally because CI is held.
3. **Findings, most important first.** For each: the problem, exact
   `file:line`, the reproduction with real output, why it matters, and
   a concrete suggested fix. Separate "fix before merge" from
   "optional". Give credit where the author's code is right and the
   gap is only in tests.
4. **The verdict and label**, stated at the end, with what unblocks it.

Rules:

- Quote real command output; never paraphrase it into something
  stronger.
- Short sentences. Plain English. Don't assume the author knows the
  codebase's internal history.
- Note pre-existing problems you found as pre-existing, so the author
  isn't blamed for them. Raise them separately.
- Don't invent problems. If it's clean, say so plainly.

## Before posting

- Re-check every head SHA against the one you reviewed.
- Post comments and labels together:

  ```sh
  gh pr comment N --body-file /tmp/slkreview/rN.md
  gh pr edit N --add-label "ready to merge" --remove-label "needs minor changes"
  ```

- Confirm the final labels, then clean up worktrees and branches.
- Report back: what was posted, any PRs that look superseded, held CI
  runs, and anything a release needs to know about.
