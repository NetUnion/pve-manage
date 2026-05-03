package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NetUnion/pve-manage/internal/api"
	"github.com/NetUnion/pve-manage/internal/config"
	"github.com/NetUnion/pve-manage/internal/db"
	"github.com/NetUnion/pve-manage/internal/pve"
	"github.com/NetUnion/pve-manage/internal/worker"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if len(args) == 0 {
		return errors.New("usage: cloud-manage <serve|worker|migrate> [flags]")
	}

	switch args[0] {
	case "serve":
		return runServe(logger, args[1:])
	case "worker":
		return runWorker(logger, args[1:])
	case "migrate":
		return runMigrate(logger, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runServe(logger *slog.Logger, args []string) error {
	runtime, err := parseRuntime(args)
	if err != nil {
		return err
	}

	cfg, err := config.Load(runtime)
	if err != nil {
		return err
	}

	dbConn, err := db.Open(runtime.DBPath)
	if err != nil {
		return err
	}
	defer dbConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.Migrate(ctx, dbConn); err != nil {
		return err
	}

	serverImpl, err := api.NewServer(context.Background(), logger, cfg, dbConn)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:    runtime.ListenAddr,
		Handler: serverImpl.Handler(),
	}

	stopCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-stopCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info("http server listening", "addr", runtime.ListenAddr)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func runWorker(logger *slog.Logger, args []string) error {
	runtime, err := parseRuntime(args)
	if err != nil {
		return err
	}

	cfg, err := config.Load(runtime)
	if err != nil {
		return err
	}

	dbConn, err := db.Open(runtime.DBPath)
	if err != nil {
		return err
	}
	defer dbConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := db.Migrate(ctx, dbConn); err != nil {
		cancel()
		return err
	}
	cancel()

	pveClient := pve.NewClient(logger, cfg.Tokens)
	w := worker.New(logger, dbConn, cfg, pveClient)

	runCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	err = w.Run(runCtx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func runMigrate(logger *slog.Logger, args []string) error {
	runtime, err := parseRuntime(args)
	if err != nil {
		return err
	}

	if _, err := config.Load(runtime); err != nil {
		return err
	}
	dbConn, err := db.Open(runtime.DBPath)
	if err != nil {
		return err
	}
	defer dbConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.Migrate(ctx, dbConn); err != nil {
		return err
	}

	logger.Info("database migrations applied", "db", runtime.DBPath)
	return nil
}

func parseRuntime(args []string) (config.Runtime, error) {
	fs := flag.NewFlagSet("cloud-manage", flag.ContinueOnError)

	runtime := config.Runtime{}
	fs.StringVar(&runtime.ListenAddr, "listen", ":8080", "HTTP listen address")
	fs.StringVar(&runtime.DBPath, "db", "cloud-manage.sqlite3", "SQLite database path")
	fs.StringVar(&runtime.ConfigPath, "config", "config/config.yaml", "main config path")
	fs.StringVar(&runtime.OIDCPath, "oidc", "config/oidc.yaml", "OIDC config path")
	fs.StringVar(&runtime.TokenPath, "token", "config/token.yaml", "PVE token config path")
	fs.StringVar(&runtime.PolicyPath, "policy", "config/policy.yaml", "optional policy config path")

	if err := fs.Parse(args); err != nil {
		return runtime, err
	}

	return runtime, nil
}
