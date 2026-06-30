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