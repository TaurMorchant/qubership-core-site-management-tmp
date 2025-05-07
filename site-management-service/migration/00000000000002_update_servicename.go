package migration

import (
	"context"
	"database/sql"
	"github.com/uptrace/bun"
)

func init() {
	log.Info("Register db evolution #2")
	migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		err := db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
			log.Info("Update tenant dns")
			if _, err := tx.Exec(`UPDATE tenant_dns dns
SET service_name = coalesce( (SELECT key from (SELECT tenant_id, value FROM  tenant_dns, lateral jsonb_each(sites)) as Val, lateral jsonb_each(Val.value) where key ~ '^(tenant\-)([0-9a-zA-Z]+$)' and Val.tenant_id=dns.tenant_id), 'tenant-dev'),
tenant_name = coalesce( (SELECT (regexp_matches(key, '[^-]+$'))[1] from (SELECT tenant_id, value FROM  tenant_dns, lateral jsonb_each(sites)) as Val, lateral jsonb_each(Val.value) where key ~ '^(tenant\-)([0-9a-zA-Z]+$)' and Val.tenant_id=dns.tenant_id), 'dev')
WHERE tenant_name IS NULL;`); err != nil {
				return err
			}
			log.Info("Evolution #2 successfully finished")
			return nil
		})
		return err
	}, func(ctx context.Context, db *bun.DB) error {
		return nil
	})
}
