# printserver

CUPS + AirSane + Avahi + a Go/React web UI in one container, deployed on AP2
(MikroTik hAP be³ Media, arm64, RouterOS 7.25.1) for a Canon PIXMA MG3620 on
USB. Read [README.md](README.md) for what runs and why, and
[DEVELOPMENT.md](DEVELOPMENT.md) for layout and commands.

## Rules

- **Never write to a router.** Router changes are runbooks in `docs/runbooks/`
  that the owner runs. Agents may read AP2 with `ssh audit@192.168.252.7` and
  RouterOS `print` commands only. The house network's details live in
  `~/Projects/home_priv` (its `local/` files are private: never copy addresses,
  MACs or credentials into this public repo).
- **The repo is public.** No house-specific addresses beyond what the
  runbooks need as placeholders, no credentials, no personal names.
- **No login, by decision.** Do not add one without the owner asking. The
  Host check and the same-origin check in `internal/api/api.go` are what
  protect ports 80 and 8090; keep both on every route. AirSane stays on
  loopback, and cupsd.conf must never get `ServerAlias *` (it disables
  cupsd's own Host check on 631).
- **`make smoke` must pass** after any change to `Dockerfile` or
  `image/rootfs/`. Unit tests cannot see CUPS, Avahi or AirSane behaving.
- **Run `make openapi`** after changing any API handler; CI checks the
  contract and GUI types.
- The image must stay buildable natively on arm64 (CI builds it on an arm64
  runner); do not add x86-only packages or drivers.
- KISS: one container, no database, settings in one JSON file.
