// Package config reads the server's settings from the environment. The
// container image sets every variable; the defaults suit `make dev-backend`.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is the process configuration.
type Config struct {
	Listen     string // HTTP listen address
	CUPSSocket string // cupsd's domain socket
	CUPSURL    string // if set, talk IPP over HTTP here instead of the socket (development)
	ESCLURL    string // AirSane's eSCL root for the scanner
	Queue      string // CUPS queue name
	DataDir    string // persistent data (settings, scans)
	Device     string // "test" or "usb"
	Hostname   string // mDNS name, without .local
	ExtraHosts []string
	// ESCLListen, if set, serves AirSane's eSCL to the network here, behind
	// the Host check, and announces it over mDNS.
	ESCLListen string
	AvahiDir   string // where the AirScan mDNS record is written
	// ScannerName is what scanning apps list.
	ScannerName string
	// USBVendor is the printer's USB vendor ID, for the connection check.
	USBVendor string
}

// FromEnv reads the configuration.
func FromEnv() (Config, error) {
	c := Config{
		Listen:     env("PRINTSERVER_LISTEN", "127.0.0.1:8080"),
		CUPSSocket: env("PRINTSERVER_CUPS_SOCKET", "/run/cups/cups.sock"),
		CUPSURL:    os.Getenv("PRINTSERVER_CUPS_URL"),
		ESCLURL:    env("PRINTSERVER_ESCL_URL", "http://127.0.0.1:8090/eSCL"),
		Queue:      env("PRINTSERVER_QUEUE", "MG3620"),
		DataDir:    env("PRINTSERVER_DATA", "./local/data"),
		Device:     env("PRINTSERVER_DEVICE", "test"),
		Hostname:   env("PRINTSERVER_HOSTNAME", "printserver"),
		ESCLListen: os.Getenv("PRINTSERVER_ESCL_LISTEN"),
		AvahiDir:   env("PRINTSERVER_AVAHI_SERVICES", "/etc/avahi/services"),
	}
	c.USBVendor = env("PRINTSERVER_USB_VENDOR", "04a9") // Canon
	c.ScannerName = env("PRINTSERVER_SCANNER_NAME", env("PRINTSERVER_PRINTER_INFO", "Canon MG3620")+" Scanner")
	for _, h := range strings.Split(os.Getenv("PRINTSERVER_ALLOWED_HOSTS"), ",") {
		if h = strings.TrimSpace(h); h != "" {
			c.ExtraHosts = append(c.ExtraHosts, h)
		}
	}
	if c.Device != "test" && c.Device != "usb" {
		return Config{}, fmt.Errorf("PRINTSERVER_DEVICE must be test or usb, got %q", c.Device)
	}
	abs, err := filepath.Abs(c.DataDir)
	if err != nil {
		return Config{}, fmt.Errorf("PRINTSERVER_DATA: %w", err)
	}
	c.DataDir = abs
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
