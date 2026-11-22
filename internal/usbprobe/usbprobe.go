// Package usbprobe tells whether the USB printer is usable from inside the
// container. RouterOS creates a passed-through device's node when the
// container starts and does not follow re-enumeration, so after a replug or
// power cycle the printer can be present (in sysfs) while the container has
// no node for it. CUPS cannot see that until a job fails, so the status page
// asks here instead.
package usbprobe

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// State is what the probe found.
type State string

const (
	Connected State = "connected" // present, with a device node
	Stale     State = "stale"     // present, but the container has no node for it: restart the container
	Missing   State = "missing"   // not on the bus: off or unplugged
	Unknown   State = "unknown"   // sysfs not readable; nothing to say
)

// Probe looks for a USB device by vendor ID.
type Probe struct {
	Vendor  string // four hex digits, e.g. "04a9" (Canon)
	SysRoot string // "/sys" in production
	DevRoot string // "/dev" in production
}

// Check reports the printer's state, and the node it found when connected.
func (p Probe) Check() (State, string) {
	devices := filepath.Join(p.SysRoot, "bus", "usb", "devices")
	entries, err := os.ReadDir(devices)
	if err != nil {
		return Unknown, ""
	}
	found := false
	for _, e := range entries {
		dir := filepath.Join(devices, e.Name())
		if !strings.EqualFold(read(dir, "idVendor"), p.Vendor) {
			continue
		}
		bus, err1 := strconv.Atoi(read(dir, "busnum"))
		dev, err2 := strconv.Atoi(read(dir, "devnum"))
		if err1 != nil || err2 != nil {
			continue
		}
		found = true
		node := filepath.Join(p.DevRoot, "bus", "usb", fmt.Sprintf("%03d", bus), fmt.Sprintf("%03d", dev))
		if _, err := os.Stat(node); err == nil {
			return Connected, node
		}
	}
	if found {
		return Stale, ""
	}
	return Missing, ""
}

func read(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
