# Printing from other networks (Work, Guest)

The printer serves the LAN plus any networks an mDNS repeater on the firewall
already covers. Phones on a separate **Work** or **Guest** VLAN cannot see or
reach it. This page is the
change to make if you decide they should.

## Two parts

1. **Reach:** a firewall pass from the network to the printer's address
   (`192.0.2.PRINT` in these examples):

   | Port | For |
   |---|---|
   | TCP 631 | Printing (IPP, AirPrint, Mopria) |
   | TCP 80 | The web UI |
   | TCP 8090 | Scanning (eSCL, AirScan) |

   On OPNsense, a rule per interface (the Work interface, the Guest
   interface), source that net, destination a host alias `hp_printserver`,
   those ports. If Work already reaches the LAN through a `net -> any` rule,
   it needs only part 2.

2. **Discovery:** phones find the printer by mDNS, which does not cross VLANs.
   The repeater on the firewall would need Work or Guest added. **The
   trade-off:** the repeater copies *every* service between the networks it
   covers, so guests would also see the LAN's AirPlay, Sonos and HomeKit names
   (not reach them: the firewall still decides that). That is a good reason
   to leave untrusted networks, such as IoT, out of the repeater.

## What not to open: the web UI

Do not give Guest (or any network you do not fully trust) TCP 80. The web UI
has no login by design, so anyone who can reach it can list, download and
delete the household's scans, change the retention, and point the
Nextcloud destination at their own server, so the household's next "send to
Nextcloud" goes to them. The same goes for TCP 8090 (scanning) on a guest
network: whoever reaches it can scan what is on the glass.

## Recommendation

- **Guest:** printing only. Pass TCP 631 to the printer and nothing else.
  Android and Windows guests can add the printer by address
  (`ipp://192.0.2.PRINT/printers/MG3620`); iPhones need discovery, which
  means the repeater trade-off above. CUPS lets any client see the queue's
  job names and cancel jobs, which is acceptable for a guest network; it does
  not let them fetch documents or administer the printer.
- **Work:** if it already reaches the LAN, web UI included, through a
  `net -> any` rule, it can print now. Add it to the repeater only if a work device must find
  the printer with AirPrint. If work devices should not see household scans,
  narrow that rule to TCP 631 first.
