<!-- OPENSPEC:START -->
# OpenSpec Instructions

These instructions are for AI assistants working in this project.

Always open `@/openspec/AGENTS.md` when the request:
- Mentions planning or proposals (words like proposal, spec, change, plan)
- Introduces new capabilities, breaking changes, architecture shifts, or big performance/security work
- Sounds ambiguous and you need the authoritative spec before coding

Use `@/openspec/AGENTS.md` to learn:
- How to create and apply change proposals
- Spec format and conventions
- Project structure and guidelines

Keep this managed block so 'openspec update' can refresh the instructions.

<!-- OPENSPEC:END -->

# Releasing

Releases are cut by pushing a `v*` tag, which triggers
`.github/workflows/release.yml` to cross-compile the binaries (via the
Makefile's `build-all`) and create a GitHub release.

**Releases are created as drafts.** `--generate-notes` only produces a raw
commit list. Before publishing, rewrite the notes into the readable, grouped
format (e.g. a `## Fixes` section in plain language, an `## Updating` section
with the `go install ...@latest` command, and a note that the first run
downloads Chromium). See the v0.5.0 release for the target format. After
editing, publish the draft manually.

The workflow intentionally does **not** call `make release`: CI authenticates
with the built-in `GH_TOKEN` (not a local `gh` login), runs Test and Build as
separate steps for clearer logs, and `make release` would re-run them. Only the
build logic (`build-all`) is shared between local and CI; the publish step is
adapted to each environment.

## Gotchas (learned the hard way)

- **`go test` does not catch cross-compilation breakage.** Tests run on the
  Unix CI host, but `build-all` cross-compiles `windows/amd64`. Unix-only
  syscalls (e.g. `syscall.Kill`, `syscall.EPERM`) compile and test green
  locally, then fail the release build on the Windows target. Before tagging,
  cross-compile every platform:
  `for p in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64 windows/amd64; do
  GOOS=${p%/*} GOARCH=${p#*/} CGO_ENABLED=0 go build -o /dev/null . || break; done`
  Put OS-specific code behind `//go:build` tags (see `process_unix.go` /
  `process_windows.go`).
- **A failed tag is not reusable, but don't force-move a pushed tag.** If the
  release build fails, the tag is already on the remote. Cut the next patch
  version (e.g. v0.5.2 failed → tag v0.5.3) rather than force-updating the
  pushed tag; burning a version number is cheaper than rewriting a ref others
  may have fetched.
- **Draft releases are owner-only and tag-less.** `gh` must be authenticated as
  the repo owner (`gh auth switch --user ZhuMon`) or the draft is invisible —
  `gh release list` and the releases API return nothing. A `--draft` release is
  also **not bound to its tag** until published: it shows up as
  `untagged-<hash>`, so `gh release view vX.Y.Z` / `gh release edit vX.Y.Z`
  return "release not found". Operate on it by **release ID** via the API:
  `gh api repos/ZhuMon/go-aws-azure-login/releases?per_page=100 --jq '.[] | select(.draft==true)'`
  to find the id, then PATCH it. Publishing is a single PATCH that flips the
  draft flag, marks it latest, and binds the tag:
  `gh api -X PATCH repos/OWNER/REPO/releases/<id> -F draft=false -f make_latest=true -f tag_name=vX.Y.Z`.