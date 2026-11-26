// terminal-share stores snapshots of home PCs' terminal windows and serves each at a
// public link that draws it.
//
// Environment:
//
//	PORT                 listen port (8080)
//	PUBLIC_URL           origin share links name (https://share.romaine.life)
//	STORAGE_ACCOUNT_URL  blob endpoint, https://<account>.blob.core.windows.net/
//	STORAGE_CONTAINER    blob container (snapshots)
//
// Flags: --memory keeps snapshots in memory instead of Azure Blob (running locally);
// --self-test checks the built image and exits (the web-app suite's image check).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/romaine-life/logline"

	"github.com/romaine-life/terminal-share/internal/auth"
	"github.com/romaine-life/terminal-share/internal/server"
	"github.com/romaine-life/terminal-share/internal/store"
	"github.com/romaine-life/terminal-share/web"
)

func main() {
	selfTest := flag.Bool("self-test", false, "check the built image's routes against stand-ins and exit")
	memory := flag.Bool("memory", false, "keep snapshots in memory instead of Azure Blob")
	flag.Parse()

	if *selfTest {
		if err := runSelfTest(); err != nil {
			fmt.Fprintf(os.Stderr, "terminal-share self-test failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("terminal-share self-test passed")
		return
	}
	if err := run(*memory); err != nil {
		stdout.Error("terminal-share stopped", "error", logline.Redact(err.Error()))
		os.Exit(1)
	}
}

// stdout is the service's log: the estate's one log line (romaine-life/logline, AHT-204), which
// the cluster ships.
var stdout = logline.New(os.Stdout)

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func run(memory bool) error {
	var snapshots store.Store
	if memory {
		snapshots = store.NewMemory()
	} else {
		accountURL := os.Getenv("STORAGE_ACCOUNT_URL")
		if accountURL == "" {
			return errors.New("STORAGE_ACCOUNT_URL is not set (or run with --memory)")
		}
		// In the cluster this is the pod's workload identity (AZURE_CLIENT_ID and
		// AZURE_FEDERATED_TOKEN_FILE, set by the workload identity webhook).
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return fmt.Errorf("azure credential: %w", err)
		}
		blob, err := store.NewBlob(accountURL, env("STORAGE_CONTAINER", "snapshots"), cred)
		if err != nil {
			return fmt.Errorf("blob store: %w", err)
		}
		snapshots = blob
	}

	srv := &server.Server{
		Store:     snapshots,
		Auth:      auth.New(auth.JWKSURL, nil, nil),
		PublicURL: env("PUBLIC_URL", "https://share.romaine.life"),
		Static:    web.Static(),
		Log:       stdout,
	}
	httpServer := &http.Server{
		Addr:              ":" + env("PORT", "8080"),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 1)
	go func() { errs <- httpServer.ListenAndServe() }()
	stdout.Info("terminal-share listening", "addr", httpServer.Addr)
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdown)
}
