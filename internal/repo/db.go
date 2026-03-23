package repo

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DBConfig struct {
	DSN      string
	MaxConns int32
}

// NewPool creates a pgxpool connection pool with sensible defaults.
func NewPool(ctx context.Context, cfg DBConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	} else {
		poolCfg.MaxConns = 25
	}
	poolCfg.MinConns = 2
	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.MaxConnIdleTime = 5 * time.Minute
	poolCfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	return pool, nil
}

// RunMigrations applies all *.up.sql files from the given directory in order.
// Uses an advisory lock so multiple instances don't race.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, migrationsDir string, logger *slog.Logger) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Release()

	// Advisory lock prevents concurrent migration runs
	const lockID = 0x4D4F535F4D494752 // "MOS_MIGR" in hex
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockID); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	defer conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockID)

	_, err = conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	rows, err := conn.Query(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("list applied: %w", err)
	}
	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.up.sql"))
	if err != nil {
		return fmt.Errorf("glob migrations: %w", err)
	}
	sort.Strings(files)

	for _, file := range files {
		version := strings.TrimSuffix(filepath.Base(file), ".up.sql")
		if applied[version] {
			continue
		}

		sqlBytes, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}

		sqlStr := string(sqlBytes)
		noTx := strings.Contains(sqlStr, "@notx")

		logger.Info("applying migration", "version", version, "notx", noTx)

		if noTx {
			noTxConn, err := pool.Acquire(ctx)
			if err != nil {
				return fmt.Errorf("acquire notx conn for %s: %w", version, err)
			}
			_, err = noTxConn.Conn().PgConn().Exec(ctx, sqlStr).ReadAll()
			noTxConn.Release()
			if err != nil {
				return fmt.Errorf("apply %s: %w", version, err)
			}
		} else {
			if _, err := conn.Exec(ctx, sqlStr); err != nil {
				return fmt.Errorf("apply %s: %w", version, err)
			}
		}

		if _, err := conn.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			return fmt.Errorf("record %s: %w", version, err)
		}

		logger.Info("migration applied", "version", version)
	}

	return nil
}
