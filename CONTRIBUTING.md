# Contributing

## Repo Layout

- This repo follows the Go CLI archetype.
- Keep the executable entrypoint in `cmd/website-packer/main.go`.
- `main.go` may contain argument parsing, AWS configuration loading, and thin output helpers; keep heavier application logic in `internal/`.
- Keep automation in `scripts/`.
- Do not introduce alternate entrypoint layouts in this repo.
