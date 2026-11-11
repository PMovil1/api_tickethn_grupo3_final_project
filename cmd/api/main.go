package main

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/auth"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/health"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/infrastructure/config"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/infrastructure/database"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/infrastructure/email"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/infrastructure/security"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/handlers"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/middleware"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/router"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("API stopped with an error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	authConfig, err := config.LoadAuth()
	if err != nil {
		return err
	}
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	check := health.NewCheck(pool, 2*time.Second)
	routes := router.New(handlers.NewHealth(check, logger))
	passwords, err := security.NewBcrypt()
	if err != nil {
		return err
	}
	codeKey, err := hkdf.Key(sha256.New, authConfig.JWTSecret, nil, "tickethn-auth-code-v1", 32)
	if err != nil {
		return err
	}
	tokens := security.NewJWT(authConfig.JWTSecret, authConfig.Issuer, authConfig.AccessTTL)
	mailer := email.NewQueue(ctx, email.NewResend(authConfig.ResendKey, authConfig.EmailFrom), logger)
	service := auth.New(database.NewAuthRepository(pool), passwords, tokens, mailer, logger, codeKey,
		authConfig.AccessTTL, authConfig.RefreshTTL)
	router.Auth(routes, handlers.NewAuth(service, logger), middleware.RequireJWT(service, logger))
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           routes,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("could not open HTTP listener: %w", err)
	}

	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(listener) }()
	logger.Info("API started", "port", cfg.Port, "environment", cfg.AppEnv)

	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server failed: %w", err)
		}
	case <-ctx.Done():
		stop()
	}

	logger.Info("Shutting down API")
	// Leave time to close the database pool within Cloud Run's 10-second grace period.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("HTTP shutdown failed: %w", errors.Join(err, server.Close()))
	}
	logger.Info("HTTP server stopped")
	return nil
}
