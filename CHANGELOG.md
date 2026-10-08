# Changelog

## Unreleased

FEATURES:

- **New resource:** `directus_collection` (tables and groups), with a provider-enforced destroy guard (`allow_destroy`, default `false`).
- **New resource:** `directus_field`, every Directus field type including alias fields; column settings are sent only when they change.
- **New resource:** `directus_relation` (m2o/o2m, m2m junctions, m2a).
- `directus_collection`, `directus_field` and `directus_relation` expose a computed `id` equal to their import ID.

## 0.1.0 (unreleased)

FEATURES:

- Provider configuration: `url` and `token` (`DIRECTUS_URL` / `DIRECTUS_TOKEN`).
- **New resource:** `directus_folder`.
