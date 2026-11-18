package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"

	"sec/internal/api"
	"sec/internal/store"
)

const serveUsage = `usage: sec serve [--port 8080] [--addr 127.0.0.1] [--data-dir DIR]

Start the SEC EDGAR & Fundamentals JSON HTTP API server.

Options:
  --port PORT       TCP port to listen on (default 8080)
  --addr HOST       Bind address (default 127.0.0.1; use 0.0.0.0 for all interfaces)
  --data-dir DIR    Directory containing the shared SQLite database
`

func RunServe(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, serveUsage)
	port := flags.Int("port", 8080, "HTTP server port")
	addr := flags.String("addr", "127.0.0.1", "HTTP bind address")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")

	if err := ParseCommandFlags(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(flags.Args()) != 0 {
		return errors.New("serve does not take positional arguments")
	}
	if *port < 1 || *port > 65535 {
		return errors.New("--port must be between 1 and 65535")
	}

	databasePath := SharedDatabasePath(*dataDir)
	srv, err := api.NewServer(databasePath)
	if err != nil {
		return fmt.Errorf("initialize API server: %w", err)
	}
	defer srv.Close()

	listenAddr := fmt.Sprintf("%s:%d", *addr, *port)
	httpServer := &http.Server{
		Addr:         listenAddr,
		Handler:      srv.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		_, _ = fmt.Fprintf(output, "SEC EDGAR API server listening on http://%s (db: %s)\n", listenAddr, databasePath)
		_, _ = fmt.Fprintln(output, "Endpoints: /api/health · /api/companies · /api/company/{ticker}/summary · /api/screener/rank · /api/sec/{ticker}/{metric}")
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case <-parent.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		_, _ = fmt.Fprintln(output, "\nServer shut down cleanly.")
		return nil
	case err := <-serverErr:
		return fmt.Errorf("http server: %w", err)
	}
}
