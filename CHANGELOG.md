# Changelog

## Unreleased

FEATURES:

- **New resource:** `directus_collection` (tables and groups), with a provider-enforced destroy guard (`allow_destroy`, default `false`).
- **New resource:** `directus_field`, every Directus field type including alias fields; column settings are sent only when they change.

## 0.1.0 (unreleased)

FEATURES:

- Provider configuration: `url` and `token` (`DIRECTUS_URL` / `DIRECTUS_TOKEN`).
- **New resource:** `directus_folder`.
