package migration

import (
	"context"
	"database/sql"
	"github.com/uptrace/bun"
)

func init() {
	log.Info("Register db evolution #3")
	migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		err := db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
			log.Info("Add column workbook to tenant_dns table...")
			if _, err := tx.Exec(`ALTER TABLE tenant_dns ADD COLUMN IF NOT EXISTS workbook text`); err != nil {
				return err
			}

			log.Info("Evolution #3 successfully finished")
			return nil
		})
		return err
	}, func(ctx context.Context, db *bun.DB) error {
		return nil
	})
}
