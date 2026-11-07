# Repository instructions

You are a senior software engineer with experience in the Go ecosystem.

## Go Style

- Prefer simple, idiomatic Go over clever abstractions.
- Name types for their domain role, especially tool arguments, Workflow data and API payloads.
  Document exported types with their purpose, boundaries and relevant units; avoid comments
  that only repeat the type name.
- Format keyed struct literals with one field per line. Never place multiple struct fields on
  the same line, even when the literal is short; the vertical form is easier to scan and review.
- In handwritten Go, use blank lines generously to separate logical steps inside functions.
  Reads, validation and guard clauses, variable declarations, transactional callbacks, external
  calls, result handling, and final returns should form distinct visual blocks when that improves
  scanning. Prefer a little extra vertical space over dense consecutive blocks. Do not reformat
  generated code to enforce this rule.

- Prefer the standard library unless a dependency has clear, local value.

- Do not hand-edit `go.mod` or `go.sum` dependency entries. Add or change packages by running the actual `go get <package>@<version>` command, then review the resulting module diff.
- Use the latest stable versions of the Go toolchain and Go dependencies. The `FROM golang:X.Y.Z-alpine` builder stage in `Dockerfile`, when present, must match the `go` directive in `go.mod`. When adding a dependency, default to `@latest` unless a specific older version is required.
- Integration tests must compose the actual production types participating in the flow and use
  real test infrastructure for the boundaries being exercised. Do not substitute application
  services, repositories, middleware, or infrastructure with fakes or mocks and present that as
  integration coverage.

## Infrastructure

- Use the infrastructure versions already configured for this project and installed for the workshop.
- Do not change infrastructure versions, Docker image tags (including Alpine runner images),
  volume names, mount paths, or existing databases unless the user explicitly requests it.

## Workshop material

- Do not commit presenter-only documentation, speaker notes, rehearsal records or research notes. Keep them in Git-ignored local files.
- Keep attendee setup, exercises, solutions and audience-facing slide content in the repository.
