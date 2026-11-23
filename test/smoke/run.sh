#!/bin/sh
# Smoke test of the real image in test mode: runs it on an isolated Docker
# network and checks, from a separate client container, what an iPhone or a
# browser on the LAN would rely on. Usage: test/smoke/run.sh [image]
set -eu

image="${1:-printserver:dev}"
net="printserver-smoke-$$"
name="printserver-smoke-$$"
out="$(mktemp -d)"

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "--- server log (last 40 lines)" >&2
    docker logs "$name" 2>&1 | grep -v ColorManager | tail -40 >&2 || true
  fi
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker network rm "$net" >/dev/null 2>&1 || true
  rm -rf "$out"
  exit "$status"
}
trap cleanup EXIT

docker buildx build --load -q -f test/smoke/client.Dockerfile -t printserver-smoke-client . >/dev/null
docker network create "$net" >/dev/null
docker run -d --name "$name" --network "$net" --hostname printserver \
  -e PRINTSERVER_PUBLIC_URL=http://print.example.com/ "$image" >/dev/null

docker run --rm --network "$net" -v "$out:/out" printserver-smoke-client sh -eu -c '
B=http://printserver.local/api/v1
fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "ok   $*"; }

i=0
until curl -sf $B/health >/dev/null 2>&1; do
  i=$((i + 1)); [ $i -lt 60 ] || fail "web API did not come up"; sleep 1
done
pass "web API answers"

i=0
while :; do
  ipp=$(avahi-browse -rtp _ipp._tcp 2>/dev/null | grep "^=" | grep IPv4 || true)
  [ -n "$ipp" ] && break
  i=$((i + 1)); [ $i -lt 10 ] || fail "no _ipp._tcp record"
  sleep 3
done
[ "$(echo "$ipp" | wc -l)" = 1 ] || fail "want one printer record (CUPS must not publish its own): $ipp"
echo "$ipp" | grep -q "URF=" || fail "no AirPrint (URF) record: $ipp"
echo "$ipp" | grep -q "Duplex=T" || fail "duplex not advertised"
echo "$ipp" | grep -q "adminurl=http://print.example.com/" || fail "printer webpage is not the web UI: $ipp"
avahi-browse -rtp _ipps._tcp 2>/dev/null | grep "^=" | grep -q IPv4 || fail "no _ipps._tcp record"
avahi-browse -rtp _universal._sub._ipp._tcp 2>/dev/null | grep -q "^=" || fail "no _universal subtype (iOS needs it)"
pass "AirPrint printer advertised"

i=0
while :; do
  n=$(avahi-browse -rtp _uscan._tcp 2>/dev/null | grep "^=" | grep -c IPv4 || true)
  [ "$n" = 1 ] && break
  i=$((i + 1)); [ $i -lt 10 ] || fail "want 1 eSCL scanner advertised, got $n"
  sleep 3
done
avahi-browse -rtp _uscan._tcp 2>/dev/null | grep "^=" | grep IPv4 | grep -q ";8090;" || fail "scanner not advertised on port 8090"
pass "AirScan scanner advertised"

curl -sf http://printserver.local:8090/eSCL/ScannerCapabilities | grep -q ScannerCapabilities || fail "eSCL capabilities"
pass "eSCL answers"

printf "Hello from the smoke test.\n" > /tmp/t.txt
lp -h printserver.local:631 -d MG3620 /tmp/t.txt >/dev/null || fail "IPP print"
pass "printed over IPP"

id=$(curl -sf -X POST -H "Content-Type: application/json" -d "{\"resolution\":150,\"color\":\"color\",\"format\":\"pdf\"}" $B/scans | jq -r .id)
[ -n "$id" ] && [ "$id" != null ] || fail "scan"
curl -sf -X POST $B/scans/$id/pages >/dev/null || fail "add page"
curl -sf -o /out/scan.pdf "$B/scans/$id/file?download=1" || fail "download scan"
pages=$(pdfinfo /out/scan.pdf | awk "/^Pages:/ {print \$2}")
[ "$pages" = 2 ] || fail "scan PDF has $pages pages, want 2"
pass "scanned a two-page PDF"

job=$(curl -sf -F file=@/out/scan.pdf -F color=monochrome -F sides=two-sided-long-edge $B/jobs | jq -r .id)
[ -n "$job" ] && [ "$job" != null ] || fail "web print"
pass "printed the scan from the web API (job $job)"

copy=$(curl -sf -X POST -H "Content-Type: application/json" -d "{\"copies\":1,\"color\":\"color\"}" $B/copy | jq -r .id)
[ -n "$copy" ] && [ "$copy" != null ] || fail "copy"
pass "copied (job $copy)"

tp=$(curl -sf -X POST $B/supplies/test-page | jq -r .id)
[ -n "$tp" ] && [ "$tp" != null ] || fail "test page"
pass "printed the test page (job $tp)"

i=0
while :; do
  left=$(curl -sf $B/jobs | jq ".active | length")
  [ "$left" = 0 ] && break
  i=$((i + 1)); [ $i -lt 90 ] || fail "$left jobs still queued after 90s"
  sleep 1
done
bad=$(curl -sf $B/jobs | jq -r "[.recent[] | select(.state != \"completed\")] | length")
[ "$bad" = 0 ] || fail "$bad jobs did not complete: $(curl -sf $B/jobs | jq -c .recent)"
pass "every job completed"

i=0
until [ "$(curl -sf $B/supplies | jq -r .last_printed)" != null ]; do
  i=$((i + 1)); [ $i -lt 40 ] || fail "last print not recorded within 2 minutes"
  sleep 3
done
[ "$(curl -sf $B/supplies | jq -r .idle_days)" = 0 ] || fail "idle days not 0 after printing"
pass "last print recorded"

code=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE -H "Sec-Fetch-Site: cross-site" $B/scans/$id)
[ "$code" = 403 ] || fail "cross-site request got $code, want 403"
code=$(curl -s -o /dev/null -w "%{http_code}" -H "Host: rebound.example" $B/status)
[ "$code" = 421 ] || fail "foreign Host on the web UI got $code, want 421"
code=$(curl -s -o /dev/null -w "%{http_code}" -H "Host: rebound.example:8090" http://printserver.local:8090/eSCL/ScannerCapabilities)
[ "$code" = 421 ] || fail "foreign Host on eSCL got $code, want 421"
code=$(curl -s -o /dev/null -w "%{http_code}" -H "Host: rebound.example:631" http://printserver.local:631/printers/MG3620)
[ "$code" = 400 ] || fail "foreign Host on CUPS got $code, want 400"
code=$(curl -s -o /dev/null -w "%{http_code}" http://printserver.local:631/printers/MG3620)
[ "$code" = 200 ] || fail "CUPS by mDNS name got $code, want 200"
code=$(curl -s -o /dev/null -w "%{http_code}" -H "Host: print.example.com" $B/status)
[ "$code" = 200 ] || fail "web UI by its public name got $code, want 200"
code=$(curl -s -o /dev/null -w "%{http_code}" -H "Host: print.example.com:631" http://printserver.local:631/printers/MG3620)
[ "$code" = 200 ] || fail "CUPS by the public name got $code, want 200"
pass "cross-site and DNS-rebinding requests refused (web UI, eSCL, CUPS)"
'
docker exec "$name" sh -c 'test -s /data/test-output/last-job.prn' || { echo "FAIL: no printer output" >&2; exit 1; }
out="$(docker exec "$name" printserver healthcheck)" || { echo "FAIL: healthcheck: $out" >&2; exit 1; }
echo "ok   healthcheck: $out"
echo "ok   printer output written by the MG3620 driver"
echo "smoke test passed"
