package usbprobe

import (
	"os"
	"path/filepath"
	"testing"
)

// fake builds a sysfs device entry and, optionally, its /dev node.
func fake(t *testing.T, root, name, vendor, bus, dev string, node bool) {
	t.Helper()
	d := filepath.Join(root, "sys", "bus", "usb", "devices", name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	for f, v := range map[string]string{"idVendor": vendor + "\n", "busnum": bus + "\n", "devnum": dev + "\n"} {
		if err := os.WriteFile(filepath.Join(d, f), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if node {
		n := filepath.Join(root, "dev", "bus", "usb", "001", "00"+dev)
		if err := os.MkdirAll(filepath.Dir(n), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(n, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func probe(root string) Probe {
	return Probe{Vendor: "04a9", SysRoot: filepath.Join(root, "sys"), DevRoot: filepath.Join(root, "dev")}
}

func TestStates(t *testing.T) {
	t.Run("connected", func(t *testing.T) {
		root := t.TempDir()
		fake(t, root, "1-1.4", "154b", "1", "3", false) // the USB drive
		fake(t, root, "1-1.2", "04a9", "1", "6", true)
		if st, node := probe(root).Check(); st != Connected || filepath.Base(node) != "006" {
			t.Errorf("got %s %s", st, node)
		}
	})
	t.Run("stale after re-enumeration", func(t *testing.T) {
		root := t.TempDir()
		fake(t, root, "1-1.2", "04a9", "1", "6", false)
		// The node the container was started with.
		if err := os.MkdirAll(filepath.Join(root, "dev", "bus", "usb", "001"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "dev", "bus", "usb", "001", "005"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if st, _ := probe(root).Check(); st != Stale {
			t.Errorf("got %s, want stale", st)
		}
	})
	t.Run("missing", func(t *testing.T) {
		root := t.TempDir()
		fake(t, root, "1-1.4", "154b", "1", "3", true)
		if st, _ := probe(root).Check(); st != Missing {
			t.Errorf("got %s, want missing", st)
		}
	})
	t.Run("no sysfs", func(t *testing.T) {
		root := t.TempDir()
		if st, _ := probe(root).Check(); st != Unknown {
			t.Errorf("got %s, want unknown", st)
		}
	})
}
