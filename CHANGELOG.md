# Changelog

## 0.1.0 (2026-10-12)

First release. Tested against Directus 12.4.

FEATURES:

- Provider configuration: `url` and `token` (`DIRECTUS_URL` / `DIRECTUS_TOKEN`). Requests are retried on HTTP 429/502/503/504, and each kind of object is read once per run and shared by every resource of that kind.
- **New resource:** `directus_collection` (tables and groups), with a provider-enforced destroy guard (`allow_destroy`, default `false`).
- **New resource:** `directus_field`, every Directus field type including alias fields; column settings are sent only when they change.
- **New resource:** `directus_relation` (m2o/o2m, m2m junctions, m2a).
- **New resource:** `directus_folder` (file-library and Flows folders).
