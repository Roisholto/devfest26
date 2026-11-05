## Go Style

- Prefer simple, idiomatic Go over clever abstractions.
- Format keyed struct literals with one field per line. Never place multiple struct fields on
  the same line, even when the literal is short; the vertical form is easier to scan and review.
- In handwritten Go, use blank lines generously to separate logical steps inside functions.
  Reads, validation and guard clauses, variable declarations, transactional callbacks, external
  calls, result handling, and final returns should form distinct visual blocks when that improves
  scanning. Prefer a little extra vertical space over dense consecutive blocks. Do not reformat
  generated code to enforce this rule.

- Prefer the standard library unless a dependency has clear, local value.

- Do not hand-edit `go.mod` or `go.sum` dependency entries. Add or change packages by running the actual `go get <package>@<version>` command, then review the resulting module diff.
- Use the latest stable versions of the Go toolchain, dependencies, and base Docker images. The `FROM golang:X.Y.Z-alpine` builder stage in `Dockerfile` must match the `go` directive in `go.mod`. When adding a dependency, default to `@latest` unless a specific older version is required. Keep base images current; `alpine:X.Y` in the runner stage should track the latest stable Alpine release.
- Integration tests must compose the actual production types participating in the flow and use
  real test infrastructure for the boundaries being exercised. Do not substitute application
  services, repositories, middleware, or infrastructure with fakes or mocks and present that as
  integration coverage.
