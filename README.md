# Terraform Provider for Directus

A Terraform provider for [Directus](https://directus.io) **configuration**: the
schema, access control, folders, flows, dashboards and settings an
administrator otherwise clicks together in the Data Studio. It does not manage
content items.

> **Status: early.** v0.1 ships the provider foundation (client, read cache,
> retries, acceptance stack) and one resource. More resources follow.

## Resources

| Resource | Purpose |
|---|---|
| `directus_collection` | A table, or a group that organises collections in the data model |
| `directus_folder` | A file-library or Flows-module folder |

Full docs are in [`docs/`](./docs/) and on the [Terraform Registry](https://registry.terraform.io/providers/Fluent-Health/directus/latest/docs).

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/install) >= 1.0
- Directus 12.4 (tested)
- [Go](https://go.dev/doc/install) 1.25+ to build from source

## Usage

```hcl
terraform {
  required_providers {
    directus = {
      source  = "Fluent-Health/directus"
      version = "~> 0.1"
    }
  }
}

provider "directus" {
  url   = "https://cms.example.com" # or DIRECTUS_URL
  token = var.directus_token        # or DIRECTUS_TOKEN: an admin user's static token
}

resource "directus_folder" "assets" {
  id   = "6f1c3a52-6a3e-4b6e-9f0e-2d7c4a1b8e90" # optional: same id in every environment
  name = "Assets"
}
```

## Design notes

- **Hand-written client** (`internal/directus`), one file per API area. It is
  not generated from `/server/specs/oas`: that spec is rendered from each
  instance's live schema, so it differs per instance.
- **Shared read cache.** Each kind of object is listed once per Terraform run
  and shared by all resources of that kind. Real instances hold thousands of
  fields and permissions; one request per resource would crawl.
- **Not-found detection.** Directus answers `403` for a missing item, the same
  as for a forbidden one. An object counts as gone only when a successful
  listing lacks it.

## Development

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

[Apache 2.0](./LICENSE)

---

Built and maintained by [Fluent Health](https://github.com/Fluent-Health).
