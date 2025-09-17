package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	gerrors "github.com/go-errors/errors"
	dbaasbase "github.com/netcracker/qubership-core-lib-go-dbaas-base-client/v3"
	"github.com/netcracker/qubership-core-lib-go-dbaas-base-client/v3/model"
	pgdbaas "github.com/netcracker/qubership-core-lib-go-dbaas-postgres-client/v4"
	"github.com/netcracker/qubership-core-lib-go/v3/configloader"
	"github.com/netcracker/qubership-core-lib-go/v3/security"
	"github.com/netcracker/qubership-core-lib-go/v3/serviceloader"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/testharness"
	"github.com/stretchr/testify/assert"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

func TestMain(m *testing.M) {
	serviceloader.Register(1, &security.DummyToken{})
	os.Exit(m.Run())
}

func TestMigration(t *testing.T) {
	testMigration(t, false)
}

func TestMigrationWithFail(t *testing.T) {
	testMigration(t, true)
}

func testMigration(t *testing.T, fail bool) {
	ctx := context.Background()
	configloader.Init(configloader.EnvPropertySource())

	pgContainer := testharness.PreparePostgres(t, ctx)
	addr, err := pgContainer.Endpoint(ctx, "")
	if err != nil {
		t.Error(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/databases") {
			w.WriteHeader(http.StatusOK)
			jsonString := pgDbaasResponseHandler(addr, testharness.PostgresPassword)
			w.Write(jsonString)
		} else {
			http.NotFoundHandler().ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	prepareTestEnvironment(server.URL)
	configloader.Init(configloader.EnvPropertySource())

	dbPool := dbaasbase.NewDbaaSPool()
	pgDbClient := pgdbaas.NewClient(dbPool)

	pgClient, err := pgDbClient.ServiceDatabase().GetPgClient()
	if err != nil {
		log.Panicf("Error occurred while creation of pgClient")
	}
	db, err := pgClient.GetBunDb(ctx)
	if err != nil {
		log.PanicC(ctx, "Can't get connection from dbaas: %v", err)
	}

	if fail {
		migrationsWithFail := createTestMigrations(true)
		migrator := migrate.NewMigrator(db, migrationsWithFail)
		err = migrator.Init(ctx)
		if err != nil {
			log.PanicC(ctx, err.Error())
		}
		err = runMigration(db, ctx, migrationsWithFail)
		if err != nil && err.Error() != "error during migration execution: fail migration manually" {
			log.PanicC(ctx, "Can't migrate existing schema: %v", err)
		}
	}

	testMigrations := createTestMigrations(false)
	migrator := migrate.NewMigrator(db, testMigrations, migrate.WithMarkAppliedOnSuccess(true))
	err = migrator.Init(ctx)
	if err != nil {
		log.PanicC(ctx, "Can't init migrator: %+v", err.Error())
	}
	err = runMigration(db, ctx, testMigrations)
	if err != nil {
		log.PanicC(ctx, "Can't migrate existing schema: %v", err)
	}

	resultSlice := make([]TestEntity2, 0)

	err = db.NewSelect().
		ColumnExpr("*").
		Model(&resultSlice).
		ModelTableExpr("test_entity").
		Scan(ctx)
	if err != nil {
		panic(err)
	}

	log.Infof("SELECT from database :%v", resultSlice)

	assert.Equal(t, 1, len(resultSlice))
	assert.Equal(t, "migration0", resultSlice[0].Migration0)
	assert.Equal(t, "migration1", resultSlice[0].Migration1)
	assert.Equal(t, "migration2", resultSlice[0].Migration2)

	if fail {
		var ms migrate.MigrationSlice

		err = db.NewSelect().
			ColumnExpr("*").
			Model(&ms).
			ModelTableExpr(bunMigrationsTable).
			Scan(ctx)
		if err != nil {
			panic(err)
		}

		assert.Equal(t, int64(1), ms[0].GroupID)
		assert.Equal(t, int64(2), ms[1].GroupID)
		assert.Equal(t, int64(2), ms[2].GroupID)
	}

	cleanTestEnvironment()
	err = pgContainer.Terminate(ctx)
	if err != nil {
		t.Fatal(err)
	}
}

func pgDbaasResponseHandler(address, password string) []byte {
	url := fmt.Sprintf("postgresql://%s/%s", address, testharness.PostgresDatabase)
	connectionProperties := map[string]interface{}{
		"password": password,
		"url":      url,
		"username": testharness.PostgresUsername,
		"role":     "admin",
	}
	dbResponse := model.LogicalDb{
		Id:                   "123",
		ConnectionProperties: connectionProperties,
	}
	jsonResponse, _ := json.Marshal(dbResponse)
	return jsonResponse
}

func prepareTestEnvironment(serverUrl string) {
	os.Setenv("dbaas.agent", serverUrl)
	os.Setenv("microservice.namespace", "site-management-namespace")
	os.Setenv("microservice.name", "site-management")
}

func cleanTestEnvironment() {
	os.Unsetenv("dbaas.agent")
}

type TestEntity0 struct {
	bun.BaseModel `bun:"test_entity"`
	Id            int32
	Migration0    string `bun:"migration0"`
}

type TestEntity1 struct {
	bun.BaseModel `bun:"test_entity"`
	Id            int32
	Migration0    string `bun:"migration0"`
	Migration1    string `bun:"migration1"`
}

type TestEntity2 struct {
	bun.BaseModel `bun:"test_entity"`
	Id            int32
	Migration0    string `bun:"migration0"`
	Migration1    string `bun:"migration1"`
	Migration2    string `bun:"migration2"`
}

func createTestMigrations(fail bool) *migrate.Migrations {
	migrations := &migrate.Migrations{}
	migration0 := migrate.Migration{Name: "00000000000000", Comment: "first_migration", Up: func(ctx context.Context, db *bun.DB, template any) error {
		log.Info("first_migration")
		err := db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
			testEntity0 := &TestEntity0{
				BaseModel:  bun.BaseModel{},
				Id:         0,
				Migration0: "migration0",
			}
			_, err := tx.NewCreateTable().Model(testEntity0).Exec(ctx)
			if err != nil {
				log.Errorf("Failed table creation")
				return gerrors.WrapPrefix(err, "failed table creation", 0)
			}
			log.Info("first_migration Insert")
			_, err = tx.NewInsert().Model(testEntity0).Exec(ctx)
			if err != nil {
				log.Errorf("Failed data insertion")
				return gerrors.WrapPrefix(err, "failed insertion", 0)
			}
			return nil
		})
		return err
	}, Down: func(ctx context.Context, db *bun.DB, template any) error { return nil }}
	migrations.Add(migration0)
	migration1 := migrate.Migration{Name: "00000000000001", Comment: "second_migration", Up: func(ctx context.Context, db *bun.DB, template any) error {
		log.Info("second_migration")
		err := db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
			testEntity1 := &TestEntity1{
				BaseModel:  bun.BaseModel{},
				Id:         0,
				Migration0: "migration0",
				Migration1: "migration1",
			}
			_, err := tx.Exec("ALTER TABLE test_entity ADD COLUMN migration1 text")
			if err != nil {
				log.Errorf("Failed adding column")
				return gerrors.WrapPrefix(err, "failed insertion", 0)
			}
			if fail {
				log.Infof("Fail migration manually")
				return errors.New("fail migration manually")
			}
			log.Info("second_migration Update")
			_, err = tx.NewUpdate().Model(testEntity1).Column("migration1").Where("id=0").Exec(ctx)
			if err != nil {
				log.Errorf("Failed data updating")
				return gerrors.WrapPrefix(err, "failed data updating", 0)
			}
			return nil
		})
		return err
	}, Down: func(ctx context.Context, db *bun.DB, template any) error { return nil }}
	migrations.Add(migration1)
	migration2 := migrate.Migration{Name: "00000000000002", Comment: "third_migration", Up: func(ctx context.Context, db *bun.DB, template any) error {
		log.Info("third_migration")
		err := db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
			testEntity2 := &TestEntity2{
				BaseModel:  bun.BaseModel{},
				Id:         0,
				Migration0: "migration0",
				Migration1: "migration1",
				Migration2: "migration2",
			}
			_, err := tx.Exec("ALTER TABLE test_entity ADD COLUMN migration2 text")
			if err != nil {
				log.Errorf("Failed adding column")
				return gerrors.WrapPrefix(err, "failed adding column", 0)
			}
			log.Info("third_migration Update")
			_, err = tx.NewUpdate().Model(testEntity2).Column("migration2").Where("id=0").Exec(ctx)
			if err != nil {
				log.Errorf("Failed data updating")
				return gerrors.WrapPrefix(err, "failed data updating", 0)
			}
			return nil
		})
		return err
	}, Down: func(ctx context.Context, db *bun.DB, template any) error { return nil }}
	migrations.Add(migration2)
	return migrations
}
