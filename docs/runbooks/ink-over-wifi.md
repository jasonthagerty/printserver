# Ink levels over the printer's Wi-Fi

The MG3620 reports no ink levels over USB (its printer interface is plain
bidirectional USB, `07/01/02`, not IPP-over-USB, and its device ID carries no
ink fields). Over its own Wi-Fi it answers standard IPP with
`marker-levels` (percent, per cartridge) and `marker-low-levels`. So printing
stays on USB, and the printer's Wi-Fi is used only for an occasional ink
reading by the print server, on an isolated network.

What the server does with `PRINTSERVER_INK_URI` set:

- reads the levels every `PRINTSERVER_INK_INTERVAL` (default `6h`);
- keeps the last good reading and its time, so a missed poll (printer
  asleep or off Wi-Fi) changes nothing;
- shows each cartridge's level on the home screen, flags "low" at the
  printer's own threshold, and says when it last checked;
- marks a cartridge replaced (install date today) when its level jumps up by
  25 points or more between readings.

## 1. An IoT SSID for the printer

A dedicated SSID on the IoT VLAN, WPA2-only so WPS can work, broadcast only by
the AP next to the printer. On the CAPsMAN controller (copying the work SSID's
configuration):

```
/interface/wifi/configuration/add copy-from=<work config> name=iot ssid=<IoT SSID> \
  datapath.vlan-id=<IoT VLAN> security.authentication-types=wpa2-psk \
  security.encryption=ccmp security.ft=no security.ft-over-ds=no \
  security.wps=push-button security.passphrase="<IoT passphrase>"
/interface/wifi/provisioning/set [find radio-mac=<AP 2.4 GHz radio MAC>] slave-configurations=<work config>,iot
/interface/wifi/radio/provision [find radio-mac=<AP 2.4 GHz radio MAC>]
/interface/wifi/print where configuration=iot
```

The MG3620 is 2.4 GHz only.

**Check the 2.4 GHz channel width before reprovisioning.** A 2.4 GHz
configuration with `channel.width=20/40/80+80mhz` keeps running until the
radio is provisioned again, then fails with "no available channels" and takes
the whole 2.4 GHz radio down. Set `channel.width=20mhz` (and the planned
frequency) on every 2.4 GHz configuration first:
`/interface/wifi/configuration/print detail where name~"2g"`.

## 2. Join the printer with WPS

1. On the printer: hold **Wi-Fi** until the **ON** lamp flashes, then release.
2. Within 2 minutes, on the CAPsMAN controller:
   `/interface/wifi/wps-push-button <the iot interface>`. Running it on the AP
   itself fails ("interface must be running in AP mode"), and on a WPA3 or
   fast-roaming SSID CAPsMAN reports "WPS is disabled".
3. Success: the Wi-Fi and ON lamps stay lit.

Its MAC (a Canon prefix, e.g. `84:BA:3B`) appears in the AP's bridge host table
on the IoT VLAN. Give it a DHCP reservation. Afterwards, set `hide-ssid=yes`
and `security.wps=disabled` on the `iot` configuration.

Check it answers, from the LAN:

```sh
ipptool -tv ipp://<printer IoT address>/ipp/print get-printer-attributes.test | grep marker
```

## 3. Only the print server may reach it

The printer's own AirPrint record stays on the IoT VLAN (it is not in the mDNS
repeater), so nobody discovers it. But a LAN with `lan net -> any` can still
reach it by address. On the firewall, on the LAN interface, above that rule:

1. pass TCP 631 from the print server's address to the printer's address;
2. block the LAN net to the printer's address.

Give the printer no internet in the IoT allowlist; it needs none.

Three things that made this not work at first, on OPNsense 26.7:

- **Aliases have their own Apply.** Saving an alias on Firewall ‣ Aliases does
  not load it; until Apply there, the rule's table is empty and the rule
  matches nothing (`pfctl -t <alias> -T show` prints nothing).
- **A floating block needs Quick.** Without it, pf treats the block as
  tentative and any later `quick` pass (every interface rule is quick) wins,
  whatever the sequence. The loaded rule must read `block drop in log quick`.
- **Beat the segment rules.** The LAN pass that actually carried the traffic
  was the segmentation rule "LAN to the segments", not `lan net -> any`; the
  live log (Firewall ‣ Log Files ‣ Live View, filtered on the printer's
  address) names the rule that passed a connection.

Check from a LAN host that is not the print server: TCP 631 and ping to the
printer should time out.

## 4. Point the server at it

On the router:

```
/container/stop printserver
/container/envs/add list=printserver key=PRINTSERVER_INK_URI value=ipp://<printer IoT address>/ipp/print
/container/start printserver
```

The log shows a reading within seconds of start (or "ink poll failed;
keeping the last reading"). The home screen's cartridge card then shows the
levels and "checked ... ago".
