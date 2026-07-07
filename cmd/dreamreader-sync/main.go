// Command dreamreader-sync is the standalone cloud-sync backend for the Dream
// Manga Reader app. It is a pure OIDC resource server: it validates access
// tokens issued by hertz-iam (via JWKS) and stores exactly one sync document per
// authenticated user in a local SQLite database. It never reads IAM storage and
// shares nothing with the main-site services beyond the IAM identity layer.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TypeDreamMoon/dreamreader-sync/internal/authmw/middleware"

	"github.com/TypeDreamMoon/dreamreader-sync/internal/config"
	"github.com/TypeDreamMoon/dreamreader-sync/internal/httpapi"
	"github.com/TypeDreamMoon/dreamreader-sync/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("dreamreader-sync exited with error", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg := config.Load()
	log.Info("starting dreamreader-sync",
		"addr", cfg.HTTPAddr, "issuer", cfg.IAMIssuer,
		"jwks", cfg.JWKSURI, "audience", cfg.ClientID, "db", cfg.DBPath)

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		return err
	}

	// The validator caches JWKS and refreshes on unknown kid. Audience is the
	// registered client_id and is mandatory: an empty audience rejects every
	// token rather than failing open.
	validator := middleware.NewValidator(middleware.Config{
		Issuer:   cfg.IAMIssuer,
		Audience: cfg.ClientID,
		JWKSURI:  cfg.JWKSURI,
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.New(cfg, st, validator, log),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
