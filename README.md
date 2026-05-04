## website-packer

This repo provides `website-packer`, a Go CLI that performs a safe “true sync” of a built website directory to an S3 bucket prefix:

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

Omit `--prefix` to publish to the bucket root.

