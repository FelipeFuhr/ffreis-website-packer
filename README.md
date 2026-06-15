<!-- ffreis-badges:start -->
[![CI](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/FelipeFuhr/ffreis-badges/main/badges/ffreis-website-packer/ci.json)](https://github.com/FelipeFuhr/ffreis-website-packer/actions)
<!-- ffreis-badges:end -->

## website-packer

This repo provides `website-packer`, a Go CLI that performs a safe "true sync" of a built website directory to an S3 bucket prefix:

- uploads/updates files under the given prefix
- optionally deletes remote keys under that prefix that no longer exist locally

### Usage

```bash
go run ./cmd/website-packer \
  --bucket my-bucket \
  --prefix sites/dev/ \
  --dir dist
```

Use `--dry-run` to preview changes or `--no-delete` to only upload/update.

To publish to the bucket root, pass `--prefix /`. Without `--no-delete`, the tool
reconciles against the entire bucket and **will delete any remote object not present
locally** — use `--dry-run` first to review the plan before applying.
