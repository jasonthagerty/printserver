# printserver

A print and scan server for one USB printer, in a single container small
enough for a MikroTik router. Phones and computers find the printer with
AirPrint and the scanner with AirScan, and a mobile-first web page prints
files, runs scans and makes copies. There is no login: everyone on the home
network can use it.

Built for a Canon PIXMA MG3620 on a MikroTik hAP be³ Media (arm64, RouterOS
7.25), but nothing in it is specific to that router.

## What runs in the container

| Part | Does |
|---|---|
| CUPS + Gutenprint | The print queue and the MG3620 driver. Shared over IPP, so iPhones (AirPrint), Android (Mopria), Macs and Windows print natively. |
| SANE + [AirSane](https://github.com/SimulPiscator/AirSane) | The scanner, as eSCL (AirScan) for Image Capture, Preview, iPhones and Windows. AirSane listens on loopback only; the Go server serves port 8090 for it. |
| Avahi | Announces the printer and scanner on the network (mDNS). The Go server writes both records, so their "webpage" links point at the web UI rather than CUPS's or AirSane's. |
| `printserver` (Go) | The web UI and its JSON API on port 80: print uploads, the queue, scanning (multi-page PDF), copying, and sending scans to Nextcloud. Also the eSCL proxy on port 8090 and its mDNS record. |
| s6-overlay | Starts and supervises the above. |

The web UI talks to CUPS over its local socket and to the scanner through
AirSane, so AirSane alone owns the scanner and phone and web scans never
collide.

## Modes

- `PRINTSERVER_DEVICE=test` (the default): the queue uses the real MG3620
  driver but writes to `/data/test-output/`, and the scanner is SANE's test
  device. For development and for the first deployment.
- `PRINTSERVER_DEVICE=usb`: the printer and scanner on USB.

## Deploying

- [Deploy to AP2](docs/runbooks/deploy-ap2.md) (test mode first)
- [Switch to the USB printer](docs/runbooks/usb-printer.md)
- [Printing from the Work and Guest networks](docs/runbooks/work-and-guest-printing.md) (optional)

Images are published to `ghcr.io/jasonthagerty/printserver` for `linux/amd64`
and `linux/arm64`; tagged releases also carry `printserver-arm64.tar` for
RouterOS's `/container/add file=`.

## Configuration

| Variable | Default (image) | Meaning |
|---|---|---|
| `PRINTSERVER_DEVICE` | `test` | `test` or `usb` |
| `PRINTSERVER_HOSTNAME` | `printserver` | mDNS name (`printserver.local`) |
| `PRINTSERVER_QUEUE` | `MG3620` | CUPS queue name |
| `PRINTSERVER_PRINTER_INFO` | `Canon MG3620` | Name phones show |
| `PRINTSERVER_LOCATION` | empty | Location phones show |
| `PRINTSERVER_PPD` | `gutenprint.5.3://bjc-PIXMA-MG3620/expert` | Driver for another printer |
| `PRINTSERVER_PAGE_SIZE` | `Letter` | Default paper |
| `PRINTSERVER_PUBLIC_URL` | `http://printserver.local/` | The web UI's address: linked as the printer's and scanner's webpage, shown in Settings, and accepted by the Host checks |
| `PRINTSERVER_ALLOWED_HOSTS` | empty | More DNS names for the web UI, scanner and CUPS, comma-separated |
| `PRINTSERVER_SCANNER_NAME` | `<printer info> Scanner` | Name scanning apps show |

Everything persistent lives under `/data`: CUPS state (so the printer keeps its
identity across upgrades), `settings.json` and `scans/`.

## Security model

The house network is the trust boundary, so there is no login. Anyone who
can reach port 80 can see, download and delete scans and change the settings;
do not open it to networks you do not trust (see the
[Work and Guest runbook](docs/runbooks/work-and-guest-printing.md)). What is
enforced:

- **Only this server's names, on every port.** Requests must name it by IP
  address, `printserver.local`, or a name in `PRINTSERVER_ALLOWED_HOSTS`. That
  stops a web page from rebinding its own DNS name to the printer and, through
  a visitor's browser, reading scans (port 80), scanning the glass (8090) or
  using the print queue (631). The Go server checks 80 and 8090; cupsd checks
  631 itself.
- **No cross-site requests.** A browser may only change things from the
  printer's own pages (checked with `Sec-Fetch-Site` and `Origin`).
- **CUPS:** anyone may print, see the queue and cancel jobs. Fetching,
  holding or moving other people's documents, and all queue administration,
  are local-only.
- **The Nextcloud app password is write-only:** the API never returns it, and
  it is dropped if the server or user changes.
- Uploads are limited to 100 MB, and only PDF, JPEG, PNG and plain text,
  detected from the content rather than the file name.

## Development

See [DEVELOPMENT.md](DEVELOPMENT.md).

## License

AGPL-3.0-or-later. See [LICENSE](LICENSE). The image bundles AirSane
(GPL-3.0), CUPS (Apache-2.0), Gutenprint (GPL-2.0-or-later) and SANE (GPL-2.0
with exception), each under its own license.
