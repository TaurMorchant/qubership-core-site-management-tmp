package migration

import (
	"context"
	"time"

	"github.com/go-errors/errors"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

var (
	relationDoesNotExistCode = "42P01"
	migrations               = migrate.NewMigrations()
	// check name of tables of migrations and locks in migrator.NewMigrator
	bunMigrationsTable     = "bun_migrations"
	bunLockMigrationTable  = "bun_migration_locks"
	bunLockMigrationColumn = "table_name"
	bunInsertLockQuery     = `insert into "` + bunLockMigrationTable + `"("` + bunLockMigrationColumn + `") values('` + bunMigrationsTable + `')`
)

type GopgMigrations struct {
	Id        int
	Version   int
	CreatedAt time.Time
}

func RunMigration(db *bun.DB, ctx context.Context) error {
	return runMigration(db, ctx, nil)
}

func runMigration(db *bun.DB, ctx context.Context, extraMigrations *migrate.Migrations) error {
	if extraMigrations != nil {
		migrations = extraMigrations
	}
	migrator := migrate.NewMigrator(db, migrations, migrate.WithMarkAppliedOnSuccess(true))
	err := migrator.Init(ctx)
	if err != nil {
		log.ErrorC(ctx, "Can't init migrator: %+v", err.Error())
		return errors.WrapPrefix(err, "can't init migrator", 0)
	}
	err = checkExistingMigrations(ctx, db, migrator)
	if err != nil {
		log.ErrorC(ctx, "Can't check existing migrations: %+v", err.Error())
		return errors.WrapPrefix(err, "can't existing migrations", 0)
	}
	log.Info("Lock migrations table")
	lockConn, err := db.Conn(ctx)
	if err != nil {
		log.ErrorC(ctx, "Error during getting lock connection: %+v", err.Error())
		return errors.WrapPrefix(err, "error during getting lock connection", 0)
	}
	defer lockConn.Close()
	lockTx, err := lockConn.BeginTx(ctx, nil)
	if err != nil {
		log.ErrorC(ctx, "Error during beginning lock transaction: %+v", err.Error())
		return errors.WrapPrefix(err, "error during beginning lock transaction", 0)
	}
	defer lockTx.Rollback()
	_, err = lockTx.Exec(bunInsertLockQuery)
	if err != nil {
		log.ErrorC(ctx, "Failed to execute lock request: %+v", err.Error())
		return errors.WrapPrefix(err, "failed to execute lock request", 0)
	}
	group, err := migrator.Migrate(ctx)
	if err != nil {
		log.ErrorC(ctx, "Error during migration execution: %+v", err.Error())
		return errors.WrapPrefix(err, "error during migration execution", 0)
	}
	if group.IsZero() {
		log.Info("There are no new migrations to run (database is up to date)")
	} else {
		log.Info("Migrated successfully for group %s", group)
	}
	return nil
}

func checkExistingMigrations(ctx context.Context, db *bun.DB, migrator *migrate.Migrator) error {
	var goPgSchemas []GopgMigrations
	err := db.NewSelect().Model(&goPgSchemas).Scan(ctx)
	if err != nil {
		log.Errorf("Error during select to table with migrations: %+v", err.Error())
		if relationDoesNotExist(err) {
			log.Debugf("There are no go-pg migrations, no check needed")
			return nil
		}
		return err
	}
	migrationsAmount := len(goPgSchemas)
	err = markExistingMigrationsAsApplied(ctx, migrationsAmount, migrator)
	return err
}

func relationDoesNotExist(err error) bool {
	return true
	//var pgErr *pgdriver.Error
	//errors.As(err, &pgErr)
	//pgErrCode := pgErr.Code
	//return strings.Compare(pgErrCode, relationDoesNotExistCode) == 0
}

func markExistingMigrationsAsApplied(ctx context.Context, migrationsAmount int, migrator *migrate.Migrator) error {
	sorted := migrations.Sorted()
	applied, _ := migrator.AppliedMigrations(ctx)
	for i := 0; i < migrationsAmount; i++ {
		if !contains(applied, sorted[i]) {
			err := migrator.MarkApplied(ctx, &sorted[i])
			if err != nil {
				log.Errorf("Error with marking existing migrations as applied %+v", err.Error())
				return err
			}
		}
	}
	return nil
}

func contains(applied migrate.MigrationSlice, migration migrate.Migration) bool {
	for _, candidate := range applied {
		if migration.Name == candidate.Name {
			return true
		}
	}
	return false
}
