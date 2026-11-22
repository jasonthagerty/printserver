# Switch to the real printer over USB

Prerequisite: [deploy-ap2.md](deploy-ap2.md) works in test mode.

RouterOS 7.20 added `devices=` to pass hardware from `/system/resource/hardware`
into a container. MikroTik documents it with a serial port only, so the first
steps are a test: if RouterOS will not pass the printer through, see
**Fallback** at the end.

## 1. Plug in and find the printer

Connect the MG3620's USB-B port to one of AP2's USB-A ports and switch the
printer on. Then:

```
/system/resource/hardware/print where type=usb
/system/resource/hardware/print detail where vendor-id="0x04a9"
```

**Check:** a Canon device (vendor `0x04a9`, name like `MG3600 series`) appears,
with a `device-name` such as `1-1.3` and a `device-path` under `bus/usb/...`.
Note the `device-name`.

## 2. Pass it to the container and switch modes

```
/container/stop printserver
/container/set printserver devices=<device-name>
/container/envs/set [find list=printserver key=PRINTSERVER_DEVICE] value=usb
/container/start printserver
/log/print follow where topics~"container"
```

**Check:** the log shows `init: mode=usb` and
`cups-queue: MG3620 -> usb://Canon/MG3600%20series?serial=...`. If it says
"no Canon printer on USB yet", the device did not reach the container (see
Troubleshooting).

## 3. Test

- The web UI's home screen no longer says Test mode, and the scanner shows
  **Canon PIXMA MG3600 Series**.
- Print a page from an iPhone and from the web UI.
- Scan a page from the web UI, and from Preview on a Mac.
- Check ink levels on the home screen. Gutenprint often reports none over USB;
  the screen then shows no gauges rather than made-up ones.

## 4. Turn off the printer's Wi-Fi

On the printer: Setup, LAN settings, Wireless LAN, Disable. This takes it off
the network, so it no longer needs Wi-Fi or an address, and nothing else can
reach it.

## Troubleshooting

- **The printer was unplugged or switched off and on.** RouterOS may give it a
  new device number, which the running container does not see. Restart the
  container: `/container/stop printserver`, then `/container/start printserver`.
  The home screen shows "The printer is off or unplugged" until then. Jobs
  already queued wait and print after the restart.
- **Nothing in `/system/resource/hardware`.** Try another USB port or cable.
  Some PIXMAs only appear while switched on.
- **The container sees no device.** In the container log, look for the
  `lpinfo` result. Run `/container/shell printserver` and then
  `ls -l /dev/bus/usb/*/`: the device node must exist. If it does not, RouterOS
  is not passing USB devices for this model; use the fallback.

## Fallback: print over the printer's Wi-Fi

If USB passthrough does not work, keep the printer's Wi-Fi on, give it a Kea
reservation, and point the queue at it instead:

- Printing: `PRINTSERVER_DEVICE=usb` still applies the right driver; set the
  queue's device to `bjnp://<printer address>` (Canon's network protocol;
  the image would need Debian's `cups-backend-bjnp` added) or the printer's own `ipp://<address>/ipp/print`.
- Scanning: SANE's pixma backend reaches the printer over the network if
  `networking=no` is removed from `/etc/sane.d/pixma.conf` and
  `bjnp://<printer address>` is added.

This needs a small change to the image (an environment variable for the
device URI). Open an issue in the repo if you get here.
