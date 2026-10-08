# Contributing

Thanks for your interest in contributing.

## Reporting bugs

Open an issue with the problem, steps to reproduce, the provider version and the Directus version.

## Proposing changes

1. Fork the repository and create a branch.
2. Make your changes with tests.
3. Open a pull request against `main`.

## Development setup

Prerequisites: Go 1.25+, Docker, Terraform CLI.

```sh
make build      # build the provider binary
make test       # unit tests
make testacc    # start a disposable Directus, run acceptance tests, tear it down
make doc        # regenerate docs/ after any schema change
```

`make testacc` uses `docker-compose.test.yml`: Directus 12.4.1 and Postgres,
with a bootstrap admin whose static token is `acceptance-admin-token`. To keep
the stack running between test runs:

```sh
docker compose -f docker-compose.test.yml up -d
./scripts/wait-for-directus.sh
TF_ACC=1 DIRECTUS_URL=http://localhost:8055 DIRECTUS_TOKEN=acceptance-admin-token \
  go test ./internal/provider/... -count=1 -v
```

### Acceptance tests run on unlicensed Directus

The test instance has **no licence key**, and CI asserts that. Keep tests
within Directus Core's limits: few collections, no licensed features. A licence
activation binds to key + `PUBLIC_URL` + project id and cannot be released
through the API, so per-run activations would use up the licence.

### Adding a resource

- Client code goes in `internal/directus/<area>.go`; Terraform code in
  `internal/provider/resource_<name>.go`.
- Read through the shared listing (`Client.list`), and invalidate it on every
  write.
- Acceptance tests cover create, update, import, and the object being deleted
  outside Terraform.

## Pull request checklist

- [ ] `make test` passes
- [ ] `make testacc` passes for changed or added resources
- [ ] `make doc` was run if schemas changed (CI fails on drift)

## Release process

1. Update `CHANGELOG.md`.
2. Tag the commit: `git tag vX.Y.Z && git push origin vX.Y.Z`.
3. The [release workflow](.github/workflows/release.yml) runs the acceptance suite, then builds, GPG-signs and publishes the GitHub release with GoReleaser.
4. The Terraform Registry picks up the new version through its GitHub webhook.

## No CLA required

By submitting a pull request, you agree to license your contribution under the repository's [LICENSE](./LICENSE).

## Maintainer one-time setup

- A GitHub Environment named `release` with secrets `GPG_PRIVATE_KEY` (ASCII-armoured) and `PASSPHRASE`.
- The repository published as a provider on [registry.terraform.io](https://registry.terraform.io) under the `Fluent-Health` namespace, with the matching GPG public key uploaded there.
