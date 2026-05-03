# Agent Context

**This repo:** `ffreis-website-packer` — Go tool (AWS SDK v2) that safely syncs a
local `dist/` directory to an S3 bucket. Used by `ffreis-data`'s self-contained
`ci.yml` to deploy ffreis.com content directly to the live bucket.

For the complete system map — how this repo relates to the deployer, the data repos,
and S3 infrastructure — see the private fleet inventory repository:

> `the fleet inventory` → `AGENTS.md`

Architecture detail (packer vs. aws s3 sync, when each is used): `AGENTS.md`
links to `docs/ARCHITECTURE.md` in the same repo.

Do not look for cross-component flow documentation in this repo's README;
it covers only the packer's own CLI flags and sync semantics.
