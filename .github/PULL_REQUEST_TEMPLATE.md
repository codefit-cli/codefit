## What this changes

A short description of the change and the motivation.

Closes #<issue-number> (if applicable)

## Checklist

- [ ] Tests added/updated and `go test ./...` passes (TDD: a failing test came
      first)
- [ ] `make lint` (golangci-lint) passes
- [ ] `CGO_ENABLED=0 go build ./...` passes (no CGO)
- [ ] Cross-compiles (linux/amd64, linux/arm64, windows/amd64)
- [ ] Self-audit stays green: `codefit scan --no-llm --fail-on critical`
- [ ] Conventional commit messages (`feat:`, `fix:`, `test:`, `docs:`, ...)
- [ ] Docs/CHANGELOG updated if user-facing

## Notes for reviewers

Anything that needs special attention, trade-offs made, or follow-ups.
