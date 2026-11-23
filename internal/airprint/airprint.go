// Package airprint publishes the print queue over mDNS (AirPrint, Mopria,
// IPP Everywhere) in place of CUPS, whose record always points "printer
// webpage" links at its own disabled web interface. The record mirrors
// what CUPS 2.4 publishes (scheduler/dirsvc.c), built from the queue's IPP
// attributes, with adminurl set to this server's web UI.
package airprint

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jasonthagerty/printserver/internal/avahi"
	"github.com/jasonthagerty/printserver/internal/cups"
)

// Announcer keeps the printer's record current.
type Announcer struct {
	CUPS     *cups.Client
	Dir      avahi.Dir
	Hostname string // mDNS host name, without .local
	AdminURL string // the web UI, linked as the printer's webpage
	Port     int    // cupsd's IPP port
	Log      *slog.Logger
}

const fileName = "airprint.service"

// Run updates the record now and then every interval until ctx ends.
func (a *Announcer) Run(ctx context.Context, interval time.Duration) {
	a.Update(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.Update(ctx)
		}
	}
}

// Update publishes the record, or removes it when the queue is gone.
func (a *Announcer) Update(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var file []byte
	p, err := a.CUPS.Printer(cctx)
	if err == nil {
		file = a.File(p)
	} else if !cups.IsNotFound(err) {
		// CUPS is restarting or busy: keep the last record rather than
		// making the printer flicker off the network.
		a.Log.Warn("AirPrint record not updated", "err", err)
		return
	}
	changed, err := a.Dir.Set(cctx, fileName, file)
	switch {
	case err != nil:
		a.Log.Warn("AirPrint record", "err", err)
	case changed && file != nil:
		a.Log.Info("AirPrint record published", "printer", p.Info, "adminurl", a.AdminURL)
	case changed:
		a.Log.Info("AirPrint record removed: no queue")
	}
}

// CUPS printer-type bits (cups/cups.h).
const (
	typeRemote    = 0x2
	typeColor     = 0x8
	typeDuplex    = 0x10
	typeCopies    = 0x40
	typeCollate   = 0x80
	typeDefault   = 0x20000
	typeRejecting = 0x80000
	typeNotShared = 0x200000
	typeMFP       = 0x4000000
)

// pdlOrder is the formats CUPS lists in "pdl", in its order.
var pdlOrder = []string{
	"application/pdf", "application/postscript", "image/jpeg", "image/png",
	"image/pwg-raster", "image/urf",
}

// File is the Avahi service file for p.
func (a *Announcer) File(p cups.Printer) []byte {
	info := p.Info
	if info == "" {
		info = p.Name
	}
	// IPP's printer-type adds state bits CUPS leaves out of the record.
	typ := (p.Type &^ (typeDefault | typeRejecting | typeNotShared)) | typeRemote
	var pdl []string
	for _, f := range pdlOrder {
		if slices.Contains(p.DocumentTypes, f) {
			pdl = append(pdl, f)
		}
	}
	product, _, _ := strings.Cut(p.MakeAndModel, " - ")
	txt := []string{
		"txtvers=1",
		"qtotal=1",
		"rp=printers/" + p.Name,
		"ty=" + p.MakeAndModel,
		"adminurl=" + a.AdminURL,
		"note=" + p.Location,
		"priority=0",
		"product=(" + product + ")",
		"pdl=" + strings.Join(pdl, ","),
		"UUID=" + strings.TrimPrefix(p.UUID, "urn:uuid:"),
		"TLS=1.2",
	}
	if len(p.URF) > 0 {
		txt = append(txt, "URF="+strings.Join(p.URF, ","))
	}
	txt = append(txt, "mopria-certified=1.3")
	for _, f := range []struct {
		bit int
		key string
	}{{typeColor, "Color"}, {typeDuplex, "Duplex"}, {typeCopies, "Copies"}, {typeCollate, "Collate"}, {typeMFP, "Scan"}} {
		if typ&f.bit != 0 {
			txt = append(txt, f.key+"=T")
		}
	}
	txt = append(txt, fmt.Sprintf("printer-type=0x%X", typ))

	// The same name CUPS used ("<info> @ <host>"), so phones and Macs that
	// already added the printer keep finding it.
	name := info + " @ " + a.Hostname
	return avahi.File(name,
		avahi.Service{Type: "_ipp._tcp", Subtypes: []string{"_universal._sub._ipp._tcp"}, Port: a.Port, TXT: txt},
		avahi.Service{Type: "_ipps._tcp", Subtypes: []string{"_universal._sub._ipps._tcp"}, Port: a.Port, TXT: txt},
		// Port 0: no LPD, which tells macOS not to try it.
		avahi.Service{Type: "_printer._tcp", Port: 0},
	)
}
