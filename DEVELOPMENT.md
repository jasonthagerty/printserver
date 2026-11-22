# Development

## Requirements

- Go (version from `go.mod`)
- Node 24. pnpm runs through `npx pnpm@10.32.1`, so it needs no global install.
- Docker with buildx, for the image and the smoke test

## Layout

```
cmd/printserver/        entrypoint: serve, openapi, version
internal/api/           huma handlers, Host and same-origin checks, GUI serving
internal/cups/          IPP client for the local cupsd (cupstest/: fake IPP server)
internal/escl/          eSCL client for AirSane (escltest/: fake scanner)
internal/airscan/       eSCL proxy on :8090 and the AirScan mDNS record
internal/scans/         scanned documents on disk; PDF assembly from JPEG pages
internal/settings/      settings.json
internal/destinations/  Nextcloud WebDAV upload
internal/config/        environment variables
web/                    React GUI; web/dist is embedded by web/embed.go
image/rootfs/           files copied into the image: s6 services, CUPS and Avahi config
test/smoke/             end-to-end test of the real image
contracts/api/v1/       generated OpenAPI document (checked in, verified in CI)
docs/runbooks/          deployment
```

## Common tasks

```sh
make test            # Go (with -race) and web tests
make lint            # gofmt, go vet, golangci-lint, eslint, prettier
make openapi         # after changing an API handler: regenerate the contract and GUI types
make smoke           # build the image and run the end-to-end smoke test
```

`make smoke` runs the image in test mode on its own Docker network and checks,
from a second container, what a phone relies on: the AirPrint and AirScan mDNS
records, printing over IPP, scanning a two-page PDF, printing and copying
through the web API, every job completing through the real MG3620 driver, and
the cross-site and DNS-rebinding refusals.

## Running locally

The web UI needs CUPS and AirSane, which only exist in the image. Either use
the image directly:

```sh
make image
docker run --rm -p 127.0.0.1:18080:80 printserver:dev   # http://localhost:18080
```

or run the image as a backend for a hot-reloading GUI:

```sh
make dev-services    # the image, with CUPS on :8631 and eSCL on :8090 (loopback)
make dev-backend     # Go server on 127.0.0.1:8080, using them
make dev-frontend    # Vite on :5173, proxying /api to the Go server
```

## Conventions

- API operations are declared with huma; the OpenAPI document is generated and
  never edited by hand. CI fails if `make openapi` was not run.
- Arrays in API responses are never null: return empty slices.
- The printer and scanner are reached only through `internal/cups` and
  `internal/escl`. Tests use the fakes in `cupstest` and `escltest`, not
  mocks of those packages.
- Format Go with `gofmt`/`goimports` and the GUI with Prettier.
