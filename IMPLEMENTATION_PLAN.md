# mdfm Implementation Plan

This plan tracks the work to position `mdfm` as a focused frontmatter manipulation library for downstream tools such as `mdid` and `mdfp`.

## Goals

- Provide stable semantic frontmatter key operations.
- Keep scope focused on frontmatter parsing/mutation/serialization and safe file updates.
- Avoid embedding domain-specific metadata logic (for example: UID generation or fingerprinting).

## Non-Goals

- Reimplementing fingerprint generation or verification.
- Reimplementing UID generation or placement policy.
- Adding tool-specific business logic for downstream consumers.

## Progress Tracking

Status legend: `todo`, `in-progress`, `done`.

## Work Items

- [x] **1. Scope and Contract Definition** (`done`)
  - Define and document operation guarantees for `Parse`, `Set`, `Get`, `Has`, `Delete`, `Keys`, and file updates.
  - Clarify newline and delimiter behavior.
  - Document strict mapping behavior and explicit error cases.

- [x] **2. Test Contract First (TDD)** (`done`)
  - Add/extend tests that lock semantic operation behavior.
  - Add idempotency tests for repeated operations.
  - Add LF/CRLF and malformed frontmatter edge case tests.

- [x] **3. API Surface Review and Additions** (`done`)
  - Confirm minimal API for downstream consumers.
  - Add focused helpers only where they reduce consumer boilerplate.
  - Keep behavior deterministic and backwards-compatible where possible.

- [x] **4. Internal Refactor for Stability** (`done`)
  - Centralize key lookup/edit internals.
  - Ensure key update and append rules are consistent.
  - Preserve semantic output guarantees.

- [x] **5. Integration-Style Validation** (`done`)
  - Add tests that mirror downstream usage patterns (set-if-missing, replace, remove-then-add).
  - Validate unchanged-content no-op behavior.

- [x] **6. Documentation Updates** (`done`)
  - Update README with scope and non-goals.
  - Add usage guidance for downstream libraries.
  - Document operation guarantees clearly.

- [ ] **7. Quality Gates** (`todo`)
  - Run `go test ./...`.
  - Run `golangci-lint run ./...` and fix findings.

- [ ] **8. Delivery and Commits** (`todo`)
  - Use conventional commits.
  - Keep commits small and reviewable.
  - Ensure lint and tests pass before each commit.

## Suggested Commit Sequence

1. `test: define semantic frontmatter operation contracts`
2. `feat: add stable semantic key operation APIs`
3. `docs: clarify scope and downstream integration patterns`

## Notes

- Update each work item status from `todo` to `in-progress` and `done` as execution progresses.
- Add links to PRs or commits next to completed items.
- 2026-03-17: Started implementation with TDD by adding `Has` contract tests,
  idempotency tests for repeated `Set`/`Delete`, and initial scope documentation
  updates in README.
- 2026-03-17: Completed contract coverage for malformed YAML frontmatter and
  newline preservation during semantic mutations (`LF` and `CRLF`).
- 2026-03-17: Added minimal downstream-focused helpers: `SetString`,
  `GetString`, `Mutate`, and `MutateString`.
- 2026-03-17: Centralized key lookup/edit internals and added order-stability
  tests for update-vs-append behavior.
- 2026-03-17: Added integration-style tests for set-if-missing, replace,
  remove-then-add flows, plus file no-op update validation.
- 2026-03-17: Expanded README with content-level mutation helpers and
  downstream integration patterns for mdid/mdfp-style workflows.
