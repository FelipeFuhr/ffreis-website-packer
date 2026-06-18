# Agent Context

**This repo:** `ffreis-website-packer` — Go tool (AWS SDK v2) that safely syncs a
local `dist/` directory to an S3 bucket, with optional CloudFront invalidation.
Used by `ffreis-data`'s self-contained `ci.yml` to deploy ffreis.com content
directly to the live bucket.

For the complete system map — how this repo relates to the deployer, the data repos,
and S3 infrastructure — see the private fleet inventory repository:

> the fleet inventory repo → `AGENTS.md`

Architecture detail (packer vs. aws s3 sync, when each is used): `AGENTS.md`
links to `docs/ARCHITECTURE.md` in the same repo.

Do not look for cross-component flow documentation in this repo's README;
it covers only the packer's own CLI flags and sync semantics.

## CLI flags

| Flag | Default | Purpose |
|------|---------|---------|
| `--bucket` | required | S3 bucket name |
| `--prefix` | required | S3 key prefix (use `/` for bucket root) |
| `--dir` | `dist` | Local directory to sync |
| `--region` | AWS default | Region override |
| `--dry-run` | false | Print plan without changing S3 |
| `--no-delete` | false | Upload/update only; skip remote-extra deletes |
| `--cloudfront-id` | (none) | CloudFront distribution ID; if set, invalidation runs after sync |
| `--cloudfront-paths` | `/*` | Comma-separated invalidation path patterns (e.g. `/css/*,/js/*`) |

## CloudFront invalidation

When `--cloudfront-id` is provided, `website-packer` calls
`cloudfront:CreateInvalidation` after a successful sync. Invalidation fires
asynchronously — the packer does not wait for completion. This replaces the
pattern of raw `aws cloudfront create-invalidation` CLI calls scattered across
CI workflows.

**Do not add raw `aws cloudfront create-invalidation` calls to new workflows.**
Use `--cloudfront-id` instead so invalidations are auditable and consistent.

Sites that previously used raw CF CLI (Phase 2 migration targets):
- a private infra repo`s deliver command — inline CloudFront SDK call (tech debt; migrate to this flag)
- a private tracker SDK repo`s cdn-publish workflow — raw aws CLI call (migrate to packer)
- `ffreis-platform-project-template/.github/workflows/deploy.yml` — raw aws CLI call (migrate to packer)

## Testing conventions

- All tests in `internal/packer/` use interface-based mocks (see `s3PutDeleteClient`,
  `cfInvalidator`) rather than the real AWS SDK. No real AWS calls in tests.
- `cloudfront_test.go` mirrors the pattern in `s3_partial_failure_test.go`.
- Run `make test` (wraps `go test -race -shuffle=on ./...`).

## Keeping this file current

- **If you discover a fact not reflected here:** add it before finishing your task.
- **If something here is wrong or outdated:** correct it in the same commit as the code change.
- **If you rename a file, command, or concept referenced here:** update the reference.
