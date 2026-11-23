# Deploy to AP2

Puts the printserver container on AP2 (hAP be³ Media, RouterOS 7.25.1) with its
own address on the house LAN. The first run uses test mode (simulated printer
and scanner), which proves the parts that do not need the printer: the image
pull from GitHub, mDNS through the bridge, AirPrint discovery on phones, and the
web UI. [usb-printer.md](usb-printer.md) switches it to the real printer.

You run every command here. Each step says how to check it and how to undo it.
Use Safe Mode (Ctrl-X in the terminal) for the bridge step, as for any bridge
change.

## What you need

- **An address for the container** on `192.168.252.0/24`, outside the Kea
  pool and unused. The commands below write it as `192.168.252.PRINT`; replace
  that everywhere. Add a Kea reservation or an Unbound host override
  (`print.<your domain>`) if you want a name; the container also announces
  `printserver.local` itself.
- **The image** published by this repo's Image workflow:
  `ghcr.io/jasonthagerty/printserver:latest`. The GHCR package must be public
  (GitHub: the package's settings, "Change visibility"), so the router can pull
  it without a token.

## 1. Format the USB drive as ext4

This erases the drive.

```
/disk/format usb1 file-system=ext4 label=printserver mbr-partition-table=yes
/disk/print detail
```

**Check:** the partition shows `fs=ext4` and a `mount-point`. The steps below
assume `usb1-part1`; if `/disk/print` shows another mount point, use it instead.

## 2. Network: a veth on the LAN

```
/interface/veth/add name=veth-print address=192.168.252.PRINT/24 gateway=192.168.252.1 comment="printserver"
/interface/bridge/port/add bridge=bridgeLocal interface=veth-print pvid=1 comment="printserver"
```

`bridgeLocal` filters VLANs, so `pvid=1` puts the container untagged on the
LAN, like `ether2`–`ether5`. No firewall or NAT rule is needed: it is a host on
the LAN.

**Check:** `/interface/bridge/vlan/print` lists `veth-print` as untagged on
VLAN 1 (the dynamic "added by pvid" row).

## 3. Container settings

```
/container/config/set registry-url=https://ghcr.io tmpdir=usb1-part1/printserver/tmp
/container/envs/add list=printserver key=PRINTSERVER_DEVICE value=test
/container/envs/add list=printserver key=PRINTSERVER_HOSTNAME value=printserver
/container/envs/add list=printserver key=TZ value=America/Chicago
/container/mounts/add list=printserver src=usb1-part1/printserver/data dst=/data
```

`TZ` sets the time shown on scans and jobs; use your own zone.

Optional, if you give it a DNS name (for example an Unbound host override
`print.example.com` → the container's address):

```
/container/envs/add list=printserver key=PRINTSERVER_PUBLIC_URL value=http://print.example.com/
```

That name becomes the "printer webpage" link on Macs and in scanning apps,
appears in Settings, and is accepted by the Host checks (the web UI, scanner
and CUPS refuse names they do not know, which stops DNS rebinding). IP
addresses and `printserver.local` always work. For more names, add
`key=PRINTSERVER_ALLOWED_HOSTS value=a.example.com,b.example.com`.

## 4. Pull and start

```
/container/add remote-image=jasonthagerty/printserver:latest interface=veth-print \
  root-dir=usb1-part1/printserver/root envlist=printserver mountlists=printserver \
  hostname=printserver dns=192.168.252.1 start-on-boot=yes logging=yes \
  memory-max=512M name=printserver comment="printserver"
/container/print
```

Wait until the status is `stopped` (the pull and extraction take a few
minutes on a USB drive), then:

```
/container/start printserver
/log/print follow where topics~"container"
```

**Check:** the log shows `init: mode=test`, `cups-queue: MG3620 -> file:///...`,
and `listening addr=:80`.

**If the pull fails**, use the release tarball instead: download
`printserver-arm64.tar` from the repo's latest GitHub release, upload it to
`usb1-part1/printserver/` (Files in WinBox or `scp`), and replace
`remote-image=...` with `file=usb1-part1/printserver/printserver-arm64.tar` in
step 4.

## 5. Verify from the house

- `http://192.168.252.PRINT/` and `http://printserver.local/` open the web UI.
  The home screen says **Test mode**.
- From blackbox: `avahi-browse -rt _ipp._tcp` shows **Canon MG3620 @
  printserver**, and `avahi-browse -rt _uscan._tcp` shows the scanner.
- On an iPhone: Share, Print. **Canon MG3620** is listed. Printing a page
  completes (the output goes to a file in test mode).
- On a Mac: Preview, File, Import from Scanner lists the test scanner.
- From a device on Media or Automation: the printer is listed too (the
  firewall's mDNS repeater forwards it).

## Updating

```
/container/stop printserver
/container/remove printserver
```

Then step 4 again. Everything that matters lives on `/data` (the mount), so a
new image keeps the queue's identity, settings and scans.

## Rollback (remove everything)

```
/container/stop printserver
/container/remove printserver
/container/mounts/remove [find list=printserver]
/container/envs/remove [find list=printserver]
/interface/bridge/port/remove [find interface=veth-print]
/interface/veth/remove veth-print
```

The data stays on the drive under `printserver/`; delete it from Files if you
want it gone.
