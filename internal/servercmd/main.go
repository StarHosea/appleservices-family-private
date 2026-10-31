package servercmd

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Laky-64/appleservices/internal/dashboard"
	"github.com/Laky-64/appleservices/internal/platform"
	"github.com/Laky-64/appleservices/nativeadi"
)

func Main() {
	secureDefaults()
	workerName := "trusted-device"
	if runtime.GOOS == "windows" {
		workerName = "trusted-device-gui"
	}
	dir := flag.String("dir", env("APPLESERVICES_DIR", "./state"), "saved device and session directory")
	worker := flag.String("worker", filepath.Join("bin", platform.Executable(workerName)), "trusted-device executable")
	library := flag.String("adi-dll", nativeadi.DefaultLibraryPath(), "verified native module")
	python := flag.String("python", "python", "64-bit Python executable")
	macPushSetup := flag.String("mac-push-setup", "./tools/mac-push-check.py", "account-isolated Mac APS bootstrap")
	port := flag.Int("port", 18741, "dashboard port when -listen is omitted")
	listen := flag.String("listen", env("APPLESERVICES_LISTEN", ""), "listen address (host:port)")
	publicURL := flag.String("public-url", env("APPLESERVICES_PUBLIC_URL", ""), "exact browser origin, e.g. https://apple.example.com")
	tokenFile := flag.String("admin-token-file", env("APPLESERVICES_ADMIN_TOKEN_FILE", ""), "file containing the administrator token")
	adminUsername := flag.String("admin-username", env("APPLESERVICES_ADMIN_USERNAME", "admin"), "administrator username for HTTP Basic authentication")
	passwordFile := flag.String("admin-password-file", env("APPLESERVICES_ADMIN_PASSWORD_FILE", ""), "file containing the HTTP Basic administrator password")
	syncTokenFile := flag.String("sync-token-file", env("APPLESERVICES_SYNC_TOKEN_FILE", ""), "file containing the read-only session synchronization token")
	healthcheck := flag.Bool("healthcheck", false, "check the local HTTP listener and exit")
	flag.Parse()
	if *healthcheck {
		address := net.JoinHostPort("127.0.0.1", fmt.Sprint(*port))
		if *listen != "" {
			host, port, err := net.SplitHostPort(*listen)
			if err != nil {
				os.Exit(1)
			}
			if host == "" || host == "0.0.0.0" {
				host = "127.0.0.1"
			} else if host == "::" {
				host = "::1"
			}
			address = net.JoinHostPort(host, port)
		}
		client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
		resp, err := client.Get("http://" + address + "/healthz")
		if err != nil {
			os.Exit(1)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	adminToken := ""
	if *tokenFile != "" {
		data, err := os.ReadFile(*tokenFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Cannot read admin token file")
			os.Exit(1)
		}
		adminToken = strings.TrimSpace(string(data))
	}
	adminPassword := ""
	if *passwordFile != "" {
		data, err := os.ReadFile(*passwordFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Cannot read admin password file")
			os.Exit(1)
		}
		adminPassword = strings.TrimSpace(string(data))
	}
	absDir, _ := filepath.Abs(*dir)
	syncToken := os.Getenv("APPLESERVICES_SYNC_TOKEN")
	os.Unsetenv("APPLESERVICES_SYNC_TOKEN")
	if *syncTokenFile != "" {
		data, err := os.ReadFile(*syncTokenFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Cannot read session sync token file")
			os.Exit(1)
		}
		syncToken = strings.TrimSpace(string(data))
	}
	absWorker, _ := filepath.Abs(*worker)
	absLibrary, _ := filepath.Abs(*library)
	absMacPushSetup, _ := filepath.Abs(*macPushSetup)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	server, err := dashboard.New(dashboard.Config{Dir: absDir, Worker: absWorker, Library: absLibrary, Python: *python, MacPushSetup: absMacPushSetup, Port: *port, Listen: *listen, PublicURL: strings.TrimRight(*publicURL, "/"), AdminToken: adminToken, AdminUsername: *adminUsername, AdminPassword: adminPassword, SyncToken: syncToken})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	appleID, password := os.Getenv("APPLESERVICES_APPLEID"), os.Getenv("APPLESERVICES_PASSWORD")
	os.Unsetenv("APPLESERVICES_APPLEID")
	os.Unsetenv("APPLESERVICES_PASSWORD")
	server.Bootstrap(ctx, appleID, password)
	fmt.Println("Local dashboard:", server.URL())
	if err := server.Serve(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Dashboard stopped")
		os.Exit(1)
	}
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
