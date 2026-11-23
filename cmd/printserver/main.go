// Command printserver serves the print and scan web UI and its API.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // the image has no zoneinfo; TZ still names scans in local time

	"github.com/jasonthagerty/printserver/internal/airprint"
	"github.com/jasonthagerty/printserver/internal/airscan"
	"github.com/jasonthagerty/printserver/internal/api"
	"github.com/jasonthagerty/printserver/internal/avahi"
	"github.com/jasonthagerty/printserver/internal/config"
	"github.com/jasonthagerty/printserver/internal/escl"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "printserver:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "serve":
		return serve()
	case "healthcheck":
		return healthcheck()
	case "openapi":
		return api.WriteOpenAPI(os.Stdout, version)
	case "version":
		fmt.Println(version)
		return nil
	default:
		return fmt.Errorf("unknown command %q (want serve, healthcheck, openapi or version)", cmd)
	}
}

func serve() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := api.NewApp(ctx, cfg, log, version)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Uploads and scans are large and slow on a phone; no write timeout.
		IdleTimeout: 2 * time.Minute,
	}
	servers := []*http.Server{srv}
	errc := make(chan error, 2)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening", "addr", cfg.Listen, "version", version, "device", cfg.Device)

	if cfg.AnnouncePrinter {
		ann := &airprint.Announcer{
			CUPS: app.CUPS(), Dir: avahi.Dir{Path: cfg.AvahiDir},
			Hostname: cfg.Hostname, AdminURL: cfg.PublicURL, Port: 631, Log: log,
		}
		go ann.Run(ctx, 30*time.Second)
	}

	if cfg.ESCLListen != "" {
		esclSrv, err := startESCL(ctx, cfg, log, errc)
		if err != nil {
			return err
		}
		servers = append(servers, esclSrv)
	}

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, s := range servers {
		if err := s.Shutdown(shutdown); err != nil {
			return err
		}
	}
	return nil
}

// startESCL serves AirSane to the network on cfg.ESCLListen and keeps its
// mDNS record current.
func startESCL(ctx context.Context, cfg config.Config, log *slog.Logger, errc chan<- error) (*http.Server, error) {
	esclURL, err := url.Parse(cfg.ESCLURL)
	if err != nil {
		return nil, fmt.Errorf("PRINTSERVER_ESCL_URL: %w", err)
	}
	target := &url.URL{Scheme: esclURL.Scheme, Host: esclURL.Host}
	_, portStr, err := net.SplitHostPort(cfg.ESCLListen)
	if err != nil {
		return nil, fmt.Errorf("PRINTSERVER_ESCL_LISTEN: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("PRINTSERVER_ESCL_LISTEN: %w", err)
	}
	srv := &http.Server{
		Addr:              cfg.ESCLListen,
		Handler:           api.ESCLHandler(target, cfg.Hostname, cfg.ExtraHosts, log),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("serving eSCL", "addr", cfg.ESCLListen, "to", target.String())

	ann := &airscan.Announcer{
		Scanner: escl.New(cfg.ESCLURL, nil), Dir: cfg.AvahiDir,
		Port: port, Hostname: cfg.Hostname, Name: cfg.ScannerName, Log: log,
		AdminURL: cfg.PublicURL + "scan",
	}
	go ann.Run(ctx, 30*time.Second)
	return srv, nil
}

// healthcheck asks the running server for its health and exits non-zero when
// it is unhealthy or not answering. RouterOS (healthcheck-cmd) and Docker
// (HEALTHCHECK) run it inside the container; its one line of output is what
// /container print shows as the health status.
func healthcheck() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return fmt.Errorf("PRINTSERVER_LISTEN: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/api/v1/health", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("web server not answering: %w", err)
	}
	defer resp.Body.Close()
	var h struct {
		Status   string   `json:"status"`
		USB      string   `json:"usb"`
		Problems []string `json:"problems"`
		Version  string   `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return fmt.Errorf("health: HTTP %s: %w", resp.Status, err)
	}
	if h.Status != "ok" {
		return fmt.Errorf("unhealthy: %s", strings.Join(h.Problems, "; "))
	}
	fmt.Printf("ok: version %s, usb %s\n", h.Version, h.USB)
	return nil
}
