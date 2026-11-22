# Switch to the real printer over USB

Prerequisite: [deploy-ap2.md](deploy-ap2.md) works in test mode.

RouterOS 7.20 added `devices=` to pass hardware from `/system/resource/hardware`
into a container. MikroTik documents it with a serial port only; on AP2
(RouterOS 7.25.1) it works for the MG3620 as below. If it ever does not, see
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
/container/set printserver devices=<device-name>:""
/container/envs/set [find list=printserver key=PRINTSERVER_DEVICE] value=usb
/container/start printserver
/log/print follow where topics~"container"
```

`devices=` takes pairs of `<router device>:<name in the container>`. The empty
name keeps the router's path (`/dev/bus/usb/001/NNN`), which is what CUPS and
SANE look for, and it follows the device number if the printer comes back
with a new one after a restart. `devices=1-1.3` alone is refused with
"expected value of container-dev-name".

**Check:** the log shows `init: mode=usb` and
`cups-queue: MG3620 -> usb://Canon/MG3600%20series?serial=...`. If it says
"no Canon printer on USB yet", the device did not reach the container (see
Troubleshooting). To look from inside:

```
/container/shell printserver
ls -l /dev/bus/usb/*/
lpinfo --include-schemes usb -v
scanimage -L
exit
```

`ls` lists one device node, `lpinfo` prints the `usb://Canon/...` URI, and
`scanimage -L` lists a `pixma:` device.

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

The home screen tells these apart (the server compares the USB bus with the
container's device nodes): "off or unplugged" or "reconnected ... restart".

- **The printer was unplugged, power-cycled, or replugged into the same
  port.** RouterOS gives it a new device number (`001/005` becomes `001/006`),
  and the running container does not get the new node. Restart the container:
  `/container/stop printserver`, then `/container/start printserver`. Jobs
  already queued wait and print after the restart.
- **The printer moved to another USB port.** The device name is the port
  (`1-1.3`, `1-1.2`, ...), so `devices=` must change too. Find the new name
  with `/system/resource/hardware/print where vendor-id="0x04a9"`, then
  `/container/stop printserver`,
  `/container/set printserver devices=<new name>:""`,
  `/container/start printserver`. Keep it on one port to avoid this.
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
