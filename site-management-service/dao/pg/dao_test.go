package pg

import (
	"context"
	"encoding/json"
	"fmt"
	dbaasbase "github.com/netcracker/qubership-core-lib-go-dbaas-base-client/v3"
	"github.com/netcracker/qubership-core-lib-go-dbaas-base-client/v3/model"
	pgdbaas "github.com/netcracker/qubership-core-lib-go-dbaas-postgres-client/v4"
	"github.com/netcracker/qubership-core-lib-go/v3/configloader"
	"github.com/netcracker/qubership-core-lib-go/v3/security"
	"github.com/netcracker/qubership-core-lib-go/v3/serviceloader"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/domain"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/migration"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/testharness"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	serviceloader.Register(1, &security.DummyToken{})
	os.Exit(m.Run())
}

type DaoTestSuit struct {
	suite.Suite
	ctx         context.Context
	pgContainer testcontainers.Container
	server      *httptest.Server
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(DaoTestSuit))
}

func (suite *DaoTestSuit) SetupSuite() {
	suite.ctx = context.Background()
	configloader.Init(configloader.EnvPropertySource())
	suite.pgContainer = testharness.PreparePostgres(suite.T(), suite.ctx)
	addr, err := suite.pgContainer.Endpoint(suite.ctx, "")
	if err != nil {
		suite.T().Error(err)
	}

	suite.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/databases") {
			w.WriteHeader(http.StatusOK)
			jsonString := pgDbaasResponseHandler(addr, testharness.PostgresPassword)
			w.Write(jsonString)
		} else {
			http.NotFoundHandler().ServeHTTP(w, r)
		}
	}))
	prepareTestEnvironment(suite.server.URL)
	configloader.Init(configloader.EnvPropertySource())
}

func (suite *DaoTestSuit) TearDownSuite() {
	cleanTestEnvironment()
	err := suite.pgContainer.Terminate(suite.ctx)
	if err != nil {
		suite.T().Fatal(err)
	}
	suite.server.Close()
}

func TestRouteManagerDao_flattenHosts(t *testing.T) {
	allSettings := &[]domain.TenantDns{{TenantId: "",
		Sites: domain.Sites{
			"central": domain.Services{
				"shopping-frontend": domain.AddressList{"ms.com/welcome", "www.ms.com/welcome"},
				"cloud-admin":       domain.AddressList{"cloud-admin.ms.com"},
			},

			"texas": domain.Services{
				"shopping-frontend": domain.AddressList{"ms-texas.com", "www.ms-texas.com"},
				"cloud-admin":       domain.AddressList{"cloud-admin.ms-texas.com"},
			},
		},
	},
	}

	actual := flattenHosts(allSettings)
	expected := utils.New("ms.com")

	if reflect.DeepEqual(actual, expected) {
		t.Fatalf("Actual: %v, Expected: %v", actual, expected)
	}
}

func (suite *DaoTestSuit) TestSetAndFindInitInformation() {
	pgClient := suite.createPgClientForTest()
	dao := NewRouteManager(pgClient)
	expectedInfo := domain.Init{Initialized: true}
	setErr := dao.SetInitInformation(suite.ctx, expectedInfo)
	assert.Nil(suite.T(), setErr)

	actualInfo, findErr := dao.FindInitInformation(suite.ctx)
	assert.Nil(suite.T(), findErr)
	assert.Equal(suite.T(), &expectedInfo, actualInfo)
	defer truncateTables(dao.pgClient, suite.ctx)
}

func (suite *DaoTestSuit) TestSaveTenant() {
	pgClient := suite.createPgClientForTest()
	dao := NewRouteManager(pgClient)
	expectedTenant := &domain.TenantDns{TenantId: "1"}
	saveErr := dao.SaveTenant(suite.ctx, expectedTenant)
	assert.Nil(suite.T(), saveErr)
	defer truncateTables(dao.pgClient, suite.ctx)
}

func (suite *DaoTestSuit) TestFindByTenantId() {
	pgClient := suite.createPgClientForTest()
	dao := NewRouteManager(pgClient)
	tenantId := "tenant-1"
	expectedTenant := &domain.TenantDns{TenantId: tenantId}
	saveErr := dao.SaveTenant(suite.ctx, expectedTenant)
	assert.Nil(suite.T(), saveErr)

	actualTenant, getErr := dao.FindByTenantId(suite.ctx, tenantId)
	assert.Nil(suite.T(), getErr)
	assert.Equal(suite.T(), expectedTenant, actualTenant)
	defer truncateTables(dao.pgClient, suite.ctx)
}

func (suite *DaoTestSuit) TestFindAllTenants() {
	pgClient := suite.createPgClientForTest()
	dao := NewRouteManager(pgClient)
	tenantId1 := "tenant-1"
	saveErr := dao.SaveTenant(suite.ctx, &domain.TenantDns{TenantId: tenantId1})
	assert.Nil(suite.T(), saveErr)

	tenantId2 := "tenant-2"
	saveErr = dao.SaveTenant(suite.ctx, &domain.TenantDns{TenantId: tenantId2})
	assert.Nil(suite.T(), saveErr)

	actualTenants, getErr := dao.FindAll(suite.ctx)
	assert.Nil(suite.T(), getErr)
	assert.Equal(suite.T(), 2, len(*actualTenants))
	defer truncateTables(dao.pgClient, suite.ctx)
}

func (suite *DaoTestSuit) TestUpsertNewEntity() {
	pgClient := suite.createPgClientForTest()
	dao := NewRouteManager(pgClient)
	tenantId := "tenant"
	expectedTenant := domain.TenantDns{TenantId: tenantId}
	saveErr := dao.Upsert(suite.ctx, expectedTenant)
	assert.Nil(suite.T(), saveErr)

	actualTenant, getErr := dao.FindByTenantId(suite.ctx, tenantId)
	assert.Nil(suite.T(), getErr)
	assert.Equal(suite.T(), &expectedTenant, actualTenant)
	defer truncateTables(dao.pgClient, suite.ctx)
}

func (suite *DaoTestSuit) TestUpsertExistingEntity() {
	pgClient := suite.createPgClientForTest()
	dao := NewRouteManager(pgClient)
	tenantId := "tenant"
	expectedTenant := domain.TenantDns{TenantId: tenantId}
	saveErr := dao.SaveTenant(suite.ctx, &expectedTenant)
	assert.Nil(suite.T(), saveErr)

	expectedTenant.TenantName = "tenant-name"

	upsErr := dao.Upsert(suite.ctx, expectedTenant)
	assert.Nil(suite.T(), upsErr)
	actualTenant, getErr := dao.FindByTenantId(suite.ctx, tenantId)
	assert.Nil(suite.T(), getErr)
	assert.Equal(suite.T(), &expectedTenant, actualTenant)
	defer truncateTables(dao.pgClient, suite.ctx)
}

func (suite *DaoTestSuit) TestDelete() {
	pgClient := suite.createPgClientForTest()
	dao := NewRouteManager(pgClient)
	tenantId := "tenant-1"
	expectedTenant := &domain.TenantDns{TenantId: tenantId}
	saveErr := dao.SaveTenant(suite.ctx, expectedTenant)
	assert.Nil(suite.T(), saveErr)

	delErr := dao.Delete(suite.ctx, tenantId)
	assert.Nil(suite.T(), delErr)

	_, getErr := dao.FindByTenantId(suite.ctx, tenantId)
	assert.NotNil(suite.T(), getErr)
	assert.Contains(suite.T(), getErr.Error(), "Tenant tenant-1 is not present in database")
	defer truncateTables(dao.pgClient, suite.ctx)
}

func (suite *DaoTestSuit) TestAddRouteToTenants() {
	pgClient := suite.createPgClientForTest()
	dao := NewRouteManager(pgClient)
	tenantId := "tenant-1"
	expectedTenant := &domain.TenantDns{TenantId: tenantId, Active: true}
	saveErr := dao.SaveTenant(suite.ctx, expectedTenant)
	assert.Nil(suite.T(), saveErr)

	serviceName := "some-service"
	addErr := dao.AddRouteToTenants(suite.ctx, "host", serviceName)
	assert.Nil(suite.T(), addErr)
	actualTenant, getErr := dao.FindByTenantId(suite.ctx, tenantId)
	assert.Nil(suite.T(), getErr)
	sites := actualTenant.Sites
	defaultSites := sites["default"]
	service := defaultSites[serviceName]
	assert.NotNil(suite.T(), service)
	defer truncateTables(dao.pgClient, suite.ctx)
}

func (suite *DaoTestSuit) TestDeleteRouteFromTenants() {
	pgClient := suite.createPgClientForTest()
	dao := NewRouteManager(pgClient)
	tenantId := "tenant-1"
	expectedTenant := &domain.TenantDns{TenantId: tenantId, Active: true}
	saveErr := dao.SaveTenant(suite.ctx, expectedTenant)
	assert.Nil(suite.T(), saveErr)

	serviceName := "some-service"
	addErr := dao.AddRouteToTenants(suite.ctx, "host", serviceName)
	assert.Nil(suite.T(), addErr)
	delErr := dao.DeleteRouteFromTenants(suite.ctx, serviceName)
	assert.Nil(suite.T(), delErr)
	actualTenant, getErr := dao.FindByTenantId(suite.ctx, tenantId)
	assert.Nil(suite.T(), getErr)
	sites := actualTenant.Sites
	defaultSites := sites["default"]
	service := defaultSites[serviceName]
	assert.Nil(suite.T(), service)
	defer truncateTables(dao.pgClient, suite.ctx)
}

func (suite *DaoTestSuit) TestFindAllHosts() {
	pgClient := suite.createPgClientForTest()
	dao := NewRouteManager(pgClient)
	tenantId := "tenant-1"
	expectedTenant := &domain.TenantDns{TenantId: tenantId, Active: true}
	saveErr := dao.SaveTenant(suite.ctx, expectedTenant)
	assert.Nil(suite.T(), saveErr)

	serviceName := "some-service"
	addErr := dao.AddRouteToTenants(suite.ctx, "host", serviceName)
	assert.Nil(suite.T(), addErr)
	set, getErr := dao.FindAllHosts(suite.ctx)
	assert.Nil(suite.T(), getErr)
	assert.NotNil(suite.T(), set)
	assert.True(suite.T(), set.Contains("host"))
	defer truncateTables(dao.pgClient, suite.ctx)
}

func (suite *DaoTestSuit) createPgClientForTest() pgdbaas.PgClient {
	dbPool := dbaasbase.NewDbaaSPool()
	pgDbClient := pgdbaas.NewClient(dbPool)

	pgClient, err := pgDbClient.ServiceDatabase().GetPgClient()
	if err != nil {
		logger.Panicf("Error occurred while creation of pgClient")
	}
	db, err := pgClient.GetBunDb(suite.ctx)
	if err != nil {
		logger.PanicC(suite.ctx, "Can't get connection from dbaas: %v", err)
	}
	err = migration.RunMigration(db, suite.ctx)
	if err != nil {
		logger.PanicC(suite.ctx, "Can't migrate existing schema: %v", err)
	}
	return pgClient
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

func truncateTables(pgClient pgdbaas.PgClient, ctx context.Context) {
	db, _ := pgClient.GetBunDb(ctx)
	var init domain.Init
	db.NewTruncateTable().Model(&init).Exec(ctx)
	var result domain.TenantDns
	db.NewTruncateTable().Model(&result).Exec(ctx)
}
