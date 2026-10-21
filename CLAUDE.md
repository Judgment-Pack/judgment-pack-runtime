# Working in this repository

This file is read by Codex as `AGENTS.md` and by Claude as `CLAUDE.md`; the two are the same text.
Edit `AGENTS.md` and run `scripts/sync-agent-docs.sh` to regenerate `CLAUDE.md`.

## Before you open a pull request

- Run `scripts/dev/check.sh` (or `jpdev check` where the kit is installed) once. It runs the
  checks listed in `dev.json` in order and prints one summary block.
- Paste that summary block in the pull request body, under a "Checks" heading.
- If a check fails: read its first lines in the block, fix the cause, re-run that one check with
  `--only NAME`, then run the whole list once more. Do not loop on the whole list.
- Start no CI watch. Push, report once, and stop; CI owns everything deterministic.
- Push with `jpdev push [BRANCH]`, or where the kit is not installed with
  `git -c credential.helper= -c 'credential.helper=!gh auth git-credential' push -u origin BRANCH`.
  The machine's stored git credential can be another account's, and a plain `git push` is then
  refused; the helper form uses the `gh` login. Never store a credential, never change global git
  configuration, never push with force (`jpdev push` refuses `--force` and `-f`).

## Before you hand over

Run these self-checks on the builder's own head before writing the commit message:

- (a) The boundary paths in `dev.json` (`handover.boundary_paths`) show no admission or verdict
  change unless the issue names one: `git diff origin/main...HEAD -- <paths>` contains additions to
  offer tables or new paths only.
- (b) Every schema member and enum value the change adds is carried by at least one vector: remove
  it from a scratch copy of the schema and a vector fails.
- (c) Where two languages read the same vectors, both readers pass over everything added.
- (d) Every mutation row added is caught, and its twin in the other language is caught by hand.
- (e) Every refusal path added emits an event that validates with a malformed input: empty,
  missing or oversized.
- (f) An interrupted and resumed run carries every new checkpoint member.
- (g) The design's words and the code agree sentence by sentence.

Run the check list with `--only <touched steps>` while iterating and whole exactly once at the end.
Change the paths in `dev.json` `handover.shared_paths` by appending only, never by reordering or
rewriting, because a sibling build may be open. Never fetch, rebase or merge; the coordinator
carries `main` into the branch after the push.

A HIGH (a path that executes twice, runs without approval, or crosses a boundary the issue did not
open) is fixed in the pull request before review. MEDIUMs named by the review of record are fixed in
the same fix round; a MEDIUM found only on a re-read goes to the pre-release batch. LOW rides.

## Rules

- Write test vectors and fixtures by hand. Never record them "from a run" of the code under test.
- Keep machine-local paths and personal names out of tracked files, commit messages and pull
  request bodies. Refer to people by role or handle where a name is needed.
- Sign off every commit (`git commit -s`). Never force-push; never rewrite published history.
- End a process you started by its process id. Never use a pattern kill (`pkill`, `killall`).
- One pull request per issue or batch. State what changed and why in plain, factual wording.
- A change to a public surface (a schema, a contract, a command's output) says so in the pull
  request and changes its vectors and documentation in the same pull request.
- If a safeguard stops you (a permission prompt, a refused command, a failed gate), stop and say
  so in your report. Do not work around it.

## Reports

Your final report is short: the pull request and its head commit, what changed, the check
summary block, and anything you stopped on or chose not to do.

## This repository

- The checks are `dev.json`, run by `scripts/dev/check.sh`; the `check` job in CI runs the same
  list. The `test` job is a matrix (one leg per operating system and Go version, the Go 1.24 floor
  included) whose leg names branch protection requires; its legs run the Go steps natively and
  are the one place the list is repeated in the workflow. A Go step added to `dev.json` is added
  to the matrix as well.
- `go.mod` carries `go 1.24.0`: the runtime reads through `os.Root`, a Go 1.24 standard-library
  type. Do not raise the floor without saying so in the pull request.
- The specification is owned by `judgment-pack-spec`; the specification artifacts embedded here
  are release-pinned and byte-exact (`.gitattributes`). Never add a runtime fetch of mutable
  specification branches.
- Every pull request carries a one-line `Material-decision impact:` declaration, and a material
  decision needs the recorded cross-vendor review described in `docs/adr/README.md`.
- Releases follow `docs/releasing.md` and `VERSIONING.md`; the release workflow is not part of
  the `check` job.
