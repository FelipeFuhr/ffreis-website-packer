# Contributing

## Repo Layout

- This repo follows the Go CLI archetype.
- Keep the executable entrypoint in `cmd/website-packer/main.go`.
- Keep application logic outside `main.go`; `main.go` should only call the top-level execute path.
- Keep automation in `scripts/`.
- Do not introduce alternate entrypoint layouts in this repo.
