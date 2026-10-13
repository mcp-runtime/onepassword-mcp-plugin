// Command onepassword-mcp-plugin is a per-user credential-injecting MCP gateway.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mcp-runtime/onepassword-mcp-plugin/internal/config"
	"github.com/mcp-runtime/onepassword-mcp-plugin/internal/credentials"
	"github.com/mcp-runtime/onepassword-mcp-plugin/internal/gateway"
	"github.com/mcp-runtime/onepassword-mcp-plugin/internal/mappings"
	"github.com/mcp-runtime/onepassword-mcp-plugin/internal/secrets"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "0.1.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "onepassword-mcp-plugin:", err)
		os.Exit(1)
	}
}

func run() error {
	path := os.Getenv("ONEPASSWORD_MCP_CONFIG")
	if path == "" {
		path = "/etc/onepassword-mcp/config.json"
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	store, err := mappings.NewFileStore(cfg.MappingsFile, cfg.RequiredFields())
	if err != nil {
		return fmt.Errorf("invalid mappings: %w", err)
	}
	resolver, err := secrets.NewOnePassword(cfg.TokenFile, cfg.IntegrationName, version, cfg.ProviderTimeout)
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	creds := &credentials.Service{Mappings: store, Resolver: resolver, TTL: cfg.CacheTTL, Max: cfg.CacheMaxEntries}
	handler := gateway.New(cfg, creds, resolver.Ready, nil)

	server := &http.Server{Addr: cfg.Listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("onepassword-mcp-plugin listening", "addr", cfg.Listen, "services", len(cfg.Services), "version", version)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
