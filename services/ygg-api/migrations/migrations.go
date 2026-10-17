// Package migrations embute os SQL do schema e os aplica com goose.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed *.sql
var FS embed.FS

// Up aplica as migrations pendentes. O advisory lock do Postgres serializa
// réplicas que sobem ao mesmo tempo (ygg-api roda com 2+ pods).
func Up(ctx context.Context, db *sql.DB) error {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, FS, goose.WithSessionLocker(locker))
	if err != nil {
		return err
	}
	results, err := p.Up(ctx)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	for _, r := range results {
		log.Printf("migration aplicada: %s (%s)", r.Source.Path, r.Duration)
	}
	return nil
}
