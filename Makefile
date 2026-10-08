default: build

build:
	go build -o terraform-provider-directus

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test ./... -count=1

# terraform-plugin-testing needs a real binary: an asdf shim fails outside a
# directory with .tool-versions (the tests run in a temp dir).
TF_ACC_TERRAFORM_PATH ?= $(shell asdf which terraform 2>/dev/null || command -v terraform)
export TF_ACC_TERRAFORM_PATH

# Starts the disposable Directus stack, runs the acceptance suite, tears it down.
testacc:
	docker compose -f docker-compose.test.yml up -d
	./scripts/wait-for-directus.sh
	TF_ACC=1 DIRECTUS_URL=http://localhost:8055 DIRECTUS_TOKEN=acceptance-admin-token \
		go test ./internal/provider/... -count=1 -v -timeout 30m; \
		rc=$$?; docker compose -f docker-compose.test.yml down -v; exit $$rc

doc:
	GOTOOLCHAIN=go1.25.8 go generate ./...

.PHONY: default build fmt vet test testacc doc
