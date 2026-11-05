package payment

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InitDatabase creates only a missing, explicitly selected local database.
// Hosted databases must be provisioned through their provider. Existing
// databases and roles are left untouched.
func InitDatabase(ctx context.Context, dsn string) error {
	cfg, err := pgxpool.ParseConfig(dsn)

	if err != nil {
		return ConnectionError(err)
	}

	c, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)

	if err == nil {
		return c.Close(ctx)
	}

	var pg *pgconn.PgError

	if !errors.As(err, &pg) || pg.Code != "3D000" {
		return ConnectionError(err)
	}

	host := cfg.ConnConfig.Host

	if host != "localhost" && host != "127.0.0.1" && host != "::1" && !strings.HasPrefix(host, "/") {
		return fmt.Errorf("configured PostgreSQL database does not exist; provision the hosted database before setup")
	}

	name := cfg.ConnConfig.Database
	maintenance := cfg.ConnConfig.Copy()
	maintenance.Database = "postgres"
	c, err = pgx.ConnectConfig(ctx, maintenance)

	if err != nil {
		return ConnectionError(err)
	}

	defer c.Close(ctx)

	_, err = c.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())

	if err != nil {
		if errors.As(err, &pg) && pg.Code == "42P04" {
			return nil
		} // A concurrent setup created it.

		return ConnectionError(err)
	}

	return nil
}

// ConnectionError reports actionable causes without printing a DSN, password,
// or an upstream error message that may contain the original connection text.
func ConnectionError(err error) error {
	var pg *pgconn.PgError

	if errors.As(err, &pg) {
		switch pg.Code {
		case "28P01", "28000":
			return fmt.Errorf("PostgreSQL authentication failed (SQLSTATE %s); check the configured role and password", pg.Code)
		case "3D000":
			return fmt.Errorf("configured PostgreSQL database does not exist (SQLSTATE 3D000)")
		default:
			return fmt.Errorf("PostgreSQL rejected the connection (SQLSTATE %s)", pg.Code)
		}
	}

	if errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("PostgreSQL connection refused; check the DATABASE_URL host/port and whether the server is running")
	}

	var dns *net.DNSError

	if errors.As(err, &dns) {
		return fmt.Errorf("PostgreSQL hostname could not be resolved; check DATABASE_URL and network connectivity")
	}

	var timeout net.Error

	if errors.As(err, &timeout) && timeout.Timeout() {
		return fmt.Errorf("PostgreSQL connection timed out; check network access and DATABASE_URL")
	}

	return fmt.Errorf("PostgreSQL connection failed; check DATABASE_URL syntax, connectivity and TLS settings")
}
