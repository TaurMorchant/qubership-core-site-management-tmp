package migration

import (
	"context"
	"database/sql"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/uptrace/bun"
)

var log = logging.GetLogger("pg-migration")

func init() {
	log.Info("Register db evolution #1")
	migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		err := db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
			log.Info("Creating table if not exists tenant_dns'...")
			if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS tenant_dns (
    		tenant_id text NOT NULL,
			tenant_admin text,
    		sites jsonb,
    		active boolean,
    		namespaces text[],
    		domain_name text,
    		service_name text,
    		tenant_name text,
    		removed boolean,
    		CONSTRAINT tenant_dns_pkey PRIMARY KEY (tenant_id)
		) WITH (
    		OIDS = FALSE
		)`); err != nil {
				return err
			}

			log.Info("Creating table if not exists inits ...")
			if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS inits (
			initialized boolean
		) WITH (
    		OIDS = FALSE
		)`); err != nil {
				return err
			}
			log.Info("Evolution #1 successfully finished")
			return nil
		})
		return err
	}, func(ctx context.Context, db *bun.DB) error {
		return nil
	})
}
