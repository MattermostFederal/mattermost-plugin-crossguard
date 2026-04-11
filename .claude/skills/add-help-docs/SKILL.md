---
name: add-help-docs
description: Use when Crossguard plugin code changes need to be reflected in user-facing documentation. Always surveys changes and writes a confirmed plan before editing. Updates the HTML help pages under public/help, the OpenAPI schema at schema/crossguard-api.yaml, and regenerates all PDFs via scripts/generate-pdfs.js. Trigger after adding or modifying REST endpoints, admin settings, slash commands, transport providers, or before cutting a release.
---

# Add Help Docs

Keep Crossguard's user-facing documentation in sync with the code. This skill covers three artifacts that must move together: HTML help pages, the OpenAPI schema, and the generated PDFs.

## When to Use

- After adding, modifying, or removing a REST endpoint in `server/api.go`
- After changing admin settings in `server/configuration.go` or `plugin.json`
- After adding or changing a slash command
- After changing transport provider behavior (NATS, Azure Queue, Azure Blob)
- After adding, renaming, or removing any error code constant in `server/errcode/codes.go`
- Before cutting a release

## When NOT to Use

- Pure refactors with no user-visible or API-surface change
- Test-only or build-only changes
- Changes already documented in a previous commit on the same branch

## Workflow

Run these steps in order. Survey first, then write and confirm a plan with the user, and only then edit any file. Do not skip the PDF regeneration step, the PDFs are checked into the repo and must match the HTML.

### Task tracking

This workflow is driven by the harness task list. Do not work without a task.

1. Start by calling `TaskList` to check for pre-existing tasks from a prior run. Reuse or delete stale tasks instead of duplicating.
2. After completing Step 1 (Survey) and Step 2 (Write and confirm plan), use `TaskCreate` to create one task per artifact identified in the approved plan. Do not invent tasks that were not in the plan. Typical tasks:
   - `Update HTML help pages for <change>` (subject), `activeForm: Updating HTML help pages`
   - `Update OpenAPI schema for <change>`, `activeForm: Updating OpenAPI schema`
   - `Regenerate help PDFs`, `activeForm: Regenerating help PDFs`
   - `Verify docs and report`, `activeForm: Verifying docs`
   Every task MUST set `subject`, `description` (what specifically is changing and why), and `activeForm`.
3. Walk the task list top-to-bottom. For each task: call `TaskUpdate` with `status: "in_progress"` **before** editing any file, do the work, then call `TaskUpdate` with `status: "completed"` only after the step's checks pass.
4. Never hold more than one task in `in_progress` at a time.
5. If blocked, leave the task `in_progress`, create a new task via `TaskCreate` describing the blocker, and move on. Do not silently skip.
6. After Step 7 (Report), call `TaskList` one more time and confirm zero `pending` or `in_progress` tasks remain. Delete any stale ones via `TaskUpdate` with `status: "deleted"` before finishing.

### 1. Survey what changed

Look at the current branch's diff against `main` and identify user-visible changes:

```bash
git --git-dir=.bare diff main...HEAD -- server/api.go server/configuration.go plugin.json server/errcode/codes.go
git --git-dir=.bare log main..HEAD --oneline
```

Focus on:
- New or changed HTTP routes, request bodies, response shapes, status codes
- New or renamed config settings, defaults, validation rules
- New slash commands or changed command arguments
- New provider options or behavioral changes in existing providers
- New, renamed, or removed constants in `server/errcode/codes.go`

Also reconcile the full set of error codes against the docs. Every constant declared in `server/errcode/codes.go` must appear as a row in `public/help/error-codes.html`, and vice versa:

```bash
grep -cE '^\s+[A-Z][A-Za-z0-9]+\s*=\s*[0-9]+$' server/errcode/codes.go
grep -cE '<td><code>[0-9]+</code>' public/help/error-codes.html
```

The two counts must match. If they differ, diff the constant names against the HTML rows and flag the drift in the plan (Step 2). Look for both directions: codes added to `codes.go` without a matching row, and rows in the HTML whose constant no longer exists in `codes.go`.

Step 1 is read-only. Do not edit any file yet.

### 2. Write and confirm plan

Before touching HTML, YAML, or PDFs, produce a short written plan and get user confirmation. The plan must cover:

- **HTML changes**: which files in `public/help/` will change, which sections or anchors are affected, what is being added/changed/removed
- **OpenAPI changes**: which paths, operations, request/response schemas, and security entries in `schema/crossguard-api.yaml` will change
- **PDF regeneration**: whether `scripts/generate-pdfs.js` will be run (it almost always will) and which PDFs are expected to change
- **Intentionally skipped**: anything that looked in-scope during Survey but will be left alone, with a one-line reason

Present the plan to the user and wait for confirmation before proceeding. If the user requests changes, revise the plan and re-confirm. The task list in the Task tracking subsection is created from this approved plan, not invented after the fact.

### 3. Update HTML help pages

All HTML sources live in `public/help/`. Match the existing tone, heading structure, and class names from `styles.css`. Never use em dashes.

| File | Scope |
|------|-------|
| `help.html` | Top-level user help landing page |
| `commands.html` | Slash commands reference |
| `admin.html` | Admin console settings and configuration |
| `api.html` | REST API reference (human-readable) |
| `transport-interface.html` | Transport providers (NATS, Azure Queue, Azure Blob) |
| `whitepaper.html` | High-level architecture and design |
| `threatmodel.html` | Security and threat model |
| `error-codes.html` | Reference for every numeric `error_code` emitted by `p.API.Log*` calls |

Only edit the pages whose scope actually changed. Preserve existing anchors so cross-links do not break.

#### Error code rows (`error-codes.html`)

When `server/errcode/codes.go` changes, update `public/help/error-codes.html` to match exactly:

- **New constant**: add a row in the section for its source file. The section's anchor ID matches the file stem (e.g., `#inbound`, `#azure-blob-provider`). Keep rows sorted by numeric code. Columns are `Code | Name / Level | Log message and context | Troubleshooting`. Read the actual call site in the referenced `server/<file>.go` to capture the verbatim log message string, the log level (DEBUG/INFO/WARN/ERROR), and the KV context fields. Do not invent descriptions.
- **Renamed constant**: the integer code is the stable contract and must not change. Update the constant name cell only.
- **Removed constant**: delete the corresponding row and update the section count in the "Range Quick Reference" table at the top.
- **New source-file block** (new 1000-range in `codes.go`): add a new `<section>` with a new anchor, add a new row to the Range Quick Reference table, and add a sidebar link if the file warrants it.
- Always update the Range Quick Reference counts when rows are added or removed so the totals stay in sync with `codes.go`.

### 4. Update the OpenAPI schema

Edit `schema/crossguard-api.yaml` so that it matches the handlers in `server/api.go` exactly:

- Paths, HTTP methods, and operation IDs
- Request body schemas and required fields
- Response schemas for success and error cases
- Security requirements (auth, permissions)
- Bump any relevant `info.version` if the plan calls for it

Use `server/api.go` as the source of truth. If the schema and the code disagree, fix the schema.

### 5. Regenerate PDFs

From the repo root:

```bash
node scripts/generate-pdfs.js
```

This uses Playwright + `pdf-lib` and writes:

- `public/help/crossguard-whitepaper.pdf`
- `public/help/crossguard-threatmodel.pdf`
- `public/help/crossguard-transport-interface.pdf`
- `public/help/crossguard-help.pdf` (merged: `help.html` + `commands.html` + `admin.html` + `api.html`)

If the script fails because Playwright browsers are missing, run `npx playwright install chromium` once, then retry.

### 6. Verify

- `ls -la public/help/*.pdf` shows fresh timestamps on all four PDFs
- `git status` shows changes under `public/help/`, `schema/crossguard-api.yaml`, and any touched HTML
- `git diff schema/crossguard-api.yaml` matches the code changes from step 1
- Spot-check one regenerated PDF opens and renders without the sidebar (the script hides it)
- Error code parity: re-run the two counts from Step 1 and confirm they still match.
  ```bash
  grep -cE '^\s+[A-Z][A-Za-z0-9]+\s*=\s*[0-9]+$' server/errcode/codes.go
  grep -cE '<td><code>[0-9]+</code>' public/help/error-codes.html
  ```

### 7. Report

Summarize in one short block:
- Which HTML files changed and why
- Which schema paths/components changed
- Which PDFs were regenerated
- Anything intentionally left alone

## Critical Files

- `public/help/*.html` — HTML sources
- `public/help/styles.css` — shared styling, do not break class names
- `schema/crossguard-api.yaml` — OpenAPI definition
- `scripts/generate-pdfs.js` — PDF generator (Playwright + pdf-lib)
- `server/api.go` — authoritative REST endpoints
- `server/configuration.go` — authoritative admin settings
- `plugin.json` — plugin manifest and settings schema

## Common Mistakes

- Editing HTML but forgetting to regenerate the PDFs. The PDFs are committed and will drift.
- Updating `api.html` but not `schema/crossguard-api.yaml` (or vice versa). Both must match `server/api.go`.
- Using em dashes. The repo convention forbids them in docs and code.
- Breaking existing anchors in HTML, which silently breaks cross-links from other pages.
- Running `generate-pdfs.js` from a subdirectory. Run it from the repo root so `HELP_DIR` resolves correctly.
- Adding a new constant to `server/errcode/codes.go` without adding a matching row in `public/help/error-codes.html`. Operators grep the code in logs and expect a reference entry.
- Guessing an error code description. Read the actual `p.API.Log*` call site so the log message string, level, and KV fields match reality.
- Renumbering or reusing an existing error code. The integer is a stable contract; only the Go identifier may be renamed.
