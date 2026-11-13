package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"pesenin.product.biz.id/api/internal/config"
	"pesenin.product.biz.id/api/internal/httpapi"
	"pesenin.product.biz.id/api/internal/migrate"
	"pesenin.product.biz.id/api/internal/push"
	"pesenin.product.biz.id/api/internal/service"
	"pesenin.product.biz.id/api/internal/storage"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cfg := config.Load()
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := runMigrate(cfg.MigrateURL); err != nil {
			slog.Error("migrate", "err", err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := runMigrate(cfg.MigrateURL); err != nil {
		slog.Error("migrate", "err", err)
		os.Exit(1)
	}
	pool, err := openPool(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	var blob storage.ObjectStore = storage.Local{Dir: cfg.StorageDir}
	if cfg.R2Enabled() {
		s3s, err := storage.NewS3(cfg.R2Endpoint, cfg.R2Bucket, cfg.R2AccessKey, cfg.R2SecretKey)
		if err != nil {
			slog.Error("r2", "err", err)
			os.Exit(1)
		}
		blob = s3s
		slog.Info("storage", "mode", "s3")
	} else {
		if err := os.MkdirAll(cfg.StorageDir, 0o755); err != nil {
			slog.Error("storage dir", "err", err)
			os.Exit(1)
		}
		slog.Info("storage", "mode", "local", "dir", cfg.StorageDir)
	}
	sender, err := push.Load(cfg.VAPIDPublic, cfg.VAPIDPrivate, cfg.VAPIDSubject, cfg.StorageDir)
	if err != nil {
		slog.Error("vapid", "err", err)
		os.Exit(1)
	}
	if cfg.VAPIDPublic == "" {
		if os.Getenv("VERCEL") == "1" {
			slog.Warn("web push disabled; set VAPID_PUBLIC_KEY and VAPID_PRIVATE_KEY")
		} else {
			slog.Warn("vapid keys were generated for local dev; set VAPID_PUBLIC_KEY and VAPID_PRIVATE_KEY in production")
		}
	}

	app := service.New(pool, cfg, blob, sender)
	if cfg.SeedDemo {
		if err := app.SeedDemo(ctx); err != nil {
			slog.Error("seed", "err", err)
			os.Exit(1)
		}
	}
	go app.RunWorker(ctx)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.New(app).Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sh)
	}()
	slog.Info("api listening", "addr", cfg.HTTPAddr, "origin", cfg.PublicOrigin)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("listen", "err", err)
		os.Exit(1)
	}
}

func runMigrate(databaseURL string) error {
	if err := migrate.Up(databaseURL); err != nil {
		return err
	}
	slog.Info("migrations applied")
	return nil
}

func openPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	// Neon's pooled host is PgBouncer in transaction mode. Prepared-statement
	// caching fails there; the simple protocol does not. Direct URLs are unchanged.
	if strings.Contains(databaseURL, "-pooler") {
		cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	}
	if os.Getenv("VERCEL") == "1" && cfg.MaxConns > 4 {
		cfg.MaxConns = 4
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}
