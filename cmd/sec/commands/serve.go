package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"sec/internal/api"
	"sec/internal/store"
)

func RunServe(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec serve [--port PORT] [--data-dir DIR] [--web DIR]")
	port := flags.Int("port", 8080, "HTTP server port")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	webDir := flags.String("web", "", "directory containing built web assets (defaults to web/dist if present)")

	if err := ParseCommandFlags(flags, args, "port", "data-dir", "web"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	databasePath := SharedDatabasePath(*dataDir)
	if _, err := os.Stat(databasePath); err != nil {
		return fmt.Errorf("database not found at %s: %w", databasePath, err)
	}

	staticDir := *webDir
	if staticDir == "" {
		// check if web/dist exists relative to current working directory
		for _, candidate := range []string{"web/dist", "../web/dist", "dist"} {
			if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
				staticDir = candidate
				break
			}
		}
	}

	srv, err := api.NewServer(databasePath, staticDir, nil)
	if err != nil {
		return fmt.Errorf("initialize server: %w", err)
	}
	defer srv.Close()

	addr := fmt.Sprintf(":%d", *port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		fmt.Fprintf(output, "SEC EDGAR Web Server running at http://localhost:%d\n", *port)
		if staticDir != "" {
			absStatic, _ := filepath.Abs(staticDir)
			fmt.Fprintf(output, "Serving frontend UI from %s\n", absStatic)
		} else {
			fmt.Fprintf(output, "Running in API mode (run 'npm run dev' inside web/ or build web/dist for UI)\n")
		}
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case <-parent.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		fmt.Fprintln(output, "\nServer shut down cleanly.")
		return nil
	case err := <-serverErr:
		return fmt.Errorf("http server: %w", err)
	}
}
