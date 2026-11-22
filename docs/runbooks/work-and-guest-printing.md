# Printing from the Work and Guest networks

Not applied. v1 serves the LAN plus the networks the OPNsense mDNS repeater
already covers (Automation, Media, WireGuard). Phones on **Work** (VLAN 10)
and **Guest** (VLAN 90) cannot see or reach the printer. This page is the
change to make if you decide they should.

## Two parts

1. **Reach:** a firewall pass from the network to the printer's address
   (`192.168.252.PRINT`):

   | Port | For |
   |---|---|
   | TCP 631 | Printing (IPP, AirPrint, Mopria) |
   | TCP 80 | The web UI |
   | TCP 8090 | Scanning (eSCL, AirScan) |

   On OPNsense, a rule per interface (`opt6` for Work, the guest interface for
   Guest), source that net, destination a host alias `hp_printserver`, those
   ports. The Work VLAN already reaches the LAN through `opt6 net -> any`, so
   Work needs only part 2.

2. **Discovery:** phones find the printer by mDNS, which does not cross VLANs.
   The repeater on the firewall would need Work or Guest added. **The
   trade-off:** the repeater copies *every* service between the networks it
   covers, so guests would also see the LAN's AirPlay, Sonos and HomeKit names
   (not reach them: the firewall still decides that). That is the reason IoT
   was left out of the repeater.

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
  (`ipp://192.168.252.PRINT/printers/MG3620`); iPhones need discovery, which
  means the repeater trade-off above. CUPS lets any client see the queue's
  job names and cancel jobs, which is acceptable for a guest network; it does
  not let them fetch documents or administer the printer.
- **Work:** it already reaches the LAN, web UI included, through
  `opt6 net -> any`. Add it to the repeater only if a work device must find
  the printer with AirPrint. If work devices should not see household scans,
  narrow that rule to TCP 631 first.
