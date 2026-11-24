package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Maxjalo/SED/services/api/internal/auth"
	"github.com/Maxjalo/SED/services/api/internal/httpapi"
	"github.com/Maxjalo/SED/services/api/internal/migrate"
	"github.com/Maxjalo/SED/services/api/internal/orch"
	"github.com/Maxjalo/SED/services/api/internal/store"
)

func main() {
	databaseURL := env("DATABASE_URL", "postgres://sed_user:sed_dev_change_me_local_only@localhost:5432/sed_db?sslmode=disable")
	fileRoot := env("FILE_STORAGE_ROOT", "/var/lib/sed/files")
	if err := os.MkdirAll(fileRoot, 0o755); err != nil {
		log.Fatalf("file root: %v", err)
	}
	maxFile := int64(50 << 20)
	if raw := os.Getenv("FILE_MAX_BYTES"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			maxFile = parsed
		}
	}

	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ping(ctx, sqlDB); err != nil {
		log.Fatalf("database: %v", err)
	}
	if err := migrate.Up(sqlDB); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	sqlDB.Close()

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	handler := httpapi.New(
		store.New(pool, fileRoot, maxFile),
		auth.New(env("AUTH_BASE_URL", "http://localhost:8091")),
		orch.New(env("ORCH_BASE_URL", "")),
	)
	port := env("API_PORT", "8080")
	log.Printf("sed-api listening on :%s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatal(err)
	}
}

func ping(ctx context.Context, db *sql.DB) error {
	var err error
	for i := 0; i < 20; i++ {
		err = db.PingContext(ctx)
		if err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return err
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
