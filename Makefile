.PHONY: frontend embed-stub build dev-backend dev-services dev-frontend test test-go test-web lint lint-go lint-web openapi verify-openapi image smoke clean

PNPM ?= npx -y pnpm@10.32.1
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO_LDFLAGS := -s -w -X main.version=$(VERSION)
IMAGE ?= printserver:dev

# Build the GUI into web/dist (embedded by the Go binary).
frontend:
	cd web && $(PNPM) install --frozen-lockfile && $(PNPM) run build

# Placeholder GUI so the Go build works without Node.
embed-stub:
	@mkdir -p web/dist
	@[ -e web/dist/index.html ] || printf '<!doctype html>\n' > web/dist/index.html

build: frontend
	CGO_ENABLED=0 go build -ldflags "$(GO_LDFLAGS)" -o printserver ./cmd/printserver

# Backend on loopback against `make dev-services` (see DEVELOPMENT.md).
dev-backend: embed-stub
	PRINTSERVER_CUPS_URL=http://127.0.0.1:8631 PRINTSERVER_ESCL_URL=http://127.0.0.1:8090/eSCL go run ./cmd/printserver

# The image in test mode, with CUPS and AirSane published on loopback.
dev-services: image
	docker rm -f printserver-dev >/dev/null 2>&1 || true
	docker run -d --name printserver-dev -p 127.0.0.1:8631:631 -p 127.0.0.1:8090:8090 -p 127.0.0.1:8081:80 $(IMAGE)

# Vite dev server; proxies /api to the backend.
dev-frontend:
	cd web && $(PNPM) run dev

test: test-go test-web

test-go: embed-stub
	go test -race ./...

test-web:
	cd web && $(PNPM) test

lint: lint-go lint-web

lint-go: embed-stub
	@test -z "$$(gofmt -l cmd internal web/embed.go)" || { gofmt -l cmd internal web/embed.go; exit 1; }
	go vet ./...
	golangci-lint run

lint-web:
	cd web && $(PNPM) run lint && $(PNPM) run format:check

# Regenerate the OpenAPI contract and the GUI's types from the Go handlers.
openapi: embed-stub
	@mkdir -p contracts/api/v1
	go run ./cmd/printserver openapi > contracts/api/v1/openapi.json
	cd web && $(PNPM) run generate:api && $(PNPM) exec prettier --write src/api/schema.d.ts >/dev/null

# Fails when a handler change was not followed by `make openapi`.
verify-openapi: openapi
	git diff --exit-code -- contracts/api/v1/openapi.json web/src/api/schema.d.ts
	@test -z "$$(git status --porcelain -- contracts/api/v1/openapi.json web/src/api/schema.d.ts)" || \
		{ echo "generated API files are untracked or modified; commit them" >&2; exit 1; }

# The image for this machine's architecture.
image:
	docker buildx build --load --build-arg VERSION=$(VERSION) -t $(IMAGE) .

# Runs the image in test mode and checks printing, scanning and discovery,
# with each driver.
smoke: image
	test/smoke/run.sh $(IMAGE)
	test/smoke/run.sh $(IMAGE) gutenprint

clean:
	rm -f printserver
	rm -rf web/dist
