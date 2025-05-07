package lib

import (
	"context"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"github.com/netcracker/qubership-core-lib-go-actuator-common/v2/health"
	"github.com/netcracker/qubership-core-lib-go-actuator-common/v2/tracing"
	dbaasbase "github.com/netcracker/qubership-core-lib-go-dbaas-base-client/v3"
	pgdbaas "github.com/netcracker/qubership-core-lib-go-dbaas-postgres-client/v4"
	"github.com/netcracker/qubership-core-lib-go-dbaas-postgres-client/v4/model"
	fiberserver "github.com/netcracker/qubership-core-lib-go-fiber-server-utils/v2"
	"github.com/netcracker/qubership-core-lib-go-fiber-server-utils/v2/server"
	"github.com/netcracker/qubership-core-lib-go-rest-utils/v2/consul-propertysource"
	routeregistration "github.com/netcracker/qubership-core-lib-go-rest-utils/v2/route-registration"
	"github.com/netcracker/qubership-core-lib-go/v3/configloader"
	constants "github.com/netcracker/qubership-core-lib-go/v3/const"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-lib-go/v3/serviceloader"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/composite"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/controller"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/dao/pg"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/docs"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/http/rest"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/idp"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/messaging"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/migration"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/paasMediationClient"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/synchronizer"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const microservice_namespace = "microservice.namespace"

var (
	ctx, globalCancel = context.WithCancel(
		context.WithValue(
			context.Background(), "requestId", "",
		),
	)
	logger = logging.GetLogger("server")
)

func RunService() {
	// TODO: Database migrations. Maybe https://github.com/golang-migrate/migrate
	// TODO: swagger

	basePropertySources := configloader.BasePropertySources(configloader.YamlPropertySourceParams{ConfigFilePath: "application.yaml"})
	configloader.InitWithSourcesArray(basePropertySources)
	consulPS := consul.NewLoggingPropertySource()
	propertySources := consul.AddConsulPropertySource(basePropertySources)
	configloader.InitWithSourcesArray(append(propertySources, consulPS))
	consul.StartWatchingForPropertiesWithRetry(context.Background(), consulPS, func(event interface{}, err error) {})

	dbPool := dbaasbase.NewDbaaSPool()
	pgDbClient := pgdbaas.NewClient(dbPool)

	dbParams := model.DbParams{
		Classifier: createSiteManagementClassifier,
	}
	pgClient, err := pgDbClient.ServiceDatabase(dbParams).GetPgClient()
	if err != nil {
		logger.Panicf("Error occurred while creation of pgClient: %v", err)
	}
	db, err := pgClient.GetBunDb(ctx)
	if err != nil {
		logger.PanicC(ctx, "Can't get connection from dbaas: %v", err)
	}
	err = migration.RunMigration(db, ctx)
	if err != nil {
		logger.PanicC(ctx, "Can't migrate existing schema: %v", err)
	}

	logger.Infof("Start data migration to PostgreSQL")
	routerDao := pg.NewRouteManager(pgClient)

	platformHostname := configloader.GetKoanf().String("cloud.public.host")
	internalGatewayAddress, err := getGatewayUrl()
	if err != nil {
		logger.Panicf("Error occurred while parsing internal Gateway URL: %v", err)
	}
	namespace := configloader.GetKoanf().MustString(microservice_namespace)

	pmClient := paasMediationClient.NewClient(ctx, internalGatewayAddress, namespace)

	mailSender, err := messaging.NewMailSender(ctx)
	if err != nil {
		logger.PanicC(ctx, "Error occurred while loading configuration for messaging: %v", err)
	}

	compositePlatformEnv := os.Getenv("COMPOSITE_PLATFORM")
	isCompositeSatellite := "true" == compositePlatformEnv
	if isCompositeSatellite {
		logger.InfoC(ctx, "Composite platform satellite mode enabled")
	}

	var baselineSM *composite.BaselineSM = nil
	if isCompositeSatellite {
		baselineNamespace := os.Getenv("BASELINE_PROJ")
		logger.InfoC(ctx, "Resolved composite baseline namespace: %s", baselineNamespace)
		if len(baselineNamespace) == 0 {
			logger.PanicC(ctx, "Failed to resolve composite baseline namespace")
		}
		baselineSmUrl := fmt.Sprintf("http://site-management.%s:8080", baselineNamespace)
		baselineSM = composite.NewBaselineSM(baselineSmUrl, rest.NewClient())
	}
	idpFacade := idp.NewFacade(namespace, serviceloader.MustLoad[idp.RetryableClient](), logging.GetLogger("idp-facade"))

	logger.InfoC(ctx, "Start routes synchronizer...")
	sync := synchronizer.New(routerDao, pmClient, mailSender, configloader.GetKoanf().Duration("synchronizer.interval"), platformHostname, pgClient, idpFacade, isCompositeSatellite, baselineSM)

	logger.InfoC(ctx, "Force initial route sync...")
	sync.Sync(ctx)

	healthService, err := health.NewHealthService()
	if err != nil {
		logger.Error("Couldn't create healthService")
	}

	app, err := fiberserver.New(fiber.Config{Network: fiber.NetworkTCP, IdleTimeout: 30 * time.Second}).
		WithPprof("6060").
		WithPrometheus("/prometheus").
		WithHealth("/health", healthService).
		WithTracer(tracing.NewZipkinTracer()).
		WithApiVersion().
		WithLogLevelsInfo().
		Process()
	if err != nil {
		logger.Error("Error while create app because: " + err.Error())
		return
	}
	controller := &controller.ApiHttpHandler{Synchronizer: sync, IDPClient: serviceloader.MustLoad[idp.RetryableClient]()}

	apiV1 := app.Group("/api/v1")
	apiV1.Post("/sync", controller.Sync)
	apiV1.Post("/validate", controller.Validate)
	apiV1.Post("/reset-caches", controller.ResetCaches)
	apiV1.Get("/public-services", controller.ListPublicServices)
	apiV1.Get("/annotated-routes", controller.ListAnnotatedRoutes)
	apiV1.Post("/annotated-routes-bulk", controller.ListAnnotatedRoutesBulk)
	apiV1.Get("/openshift-routes", controller.ListOpenShiftRoutes)
	apiV1.Get("/identity-provider-route", controller.GetIdpRoute)
	apiV1.Get("/trusted-hosts/:tenantId", controller.GetRealm)
	apiV1.Get("/trusted-hosts", controller.GetRealms)
	apiV1.Get("/tenants/current/service/name", controller.GetServiceName)
	apiV1.Get("/tenants/current/services", controller.GetTenantCurrentServices)
	apiV1.Post("/tenants", controller.RegisterTenant)
	apiV1.Delete("/tenants/:tenantId", controller.DeleteTenant)
	apiV1.Get("/search", controller.Search)
	apiV1.Post("/activate/create-os-tenant-alias-routes/perform/:tenantId", controller.CreateTenantRoute)

	apiRoutes := apiV1.Group("/routes")
	apiRoutes.Post("/sync-idp", controller.SyncIDP)
	apiRoutes.Get("/:tenantId/site", controller.GetSite)
	apiRoutes.Get("/:tenantId", controller.Get)
	apiRoutes.Get("/", controller.GetAll)
	apiRoutes.Post("/:tenantId/activate", controller.ActivateTenant)
	apiRoutes.Post("/:tenantId/deactivate", controller.DeactivateTenant)
	apiRoutes.Post("/", controller.Upsert)
	apiRoutes.Put("/", controller.UpsertUpdate)
	apiRoutes.Delete("/:tenantId", controller.Delete)
	apiRoutes.Post("/:tenantId/restore-tenant-alias", controller.RestoreTenantAlias)

	routeregistration.NewRegistrar().WithRoutes(
		routeregistration.Route{From: "/api/v1/site-management", To: "/api/v1", RouteType: routeregistration.Public},
		routeregistration.Route{From: "/api/v4/tenant-manager/manage/tenants/current/service/name",
			To: "/api/v1/tenants/current/service/name", RouteType: routeregistration.Public},
		routeregistration.Route{From: "/api/v4/tenant-manager/activate/create-os-tenant-alias-routes/perform/{tenantId}",
			To: "/api/v1/activate/create-os-tenant-alias-routes/perform/{tenantId}", RouteType: routeregistration.Internal},
	).Register()

	// swagger
	app.Get("/swagger-ui/swagger.json", func(ctx *fiber.Ctx) error {
		ctx.Set("Content-Type", "application/json")
		return ctx.Status(http.StatusOK).SendString(docs.SwaggerInfo.ReadDoc()) // run `go generate` for local work
	})

	registerShutdownHook(func() {
		if err := app.Shutdown(); err != nil {
			logger.ErrorC(ctx, "Site-management error during server shutdown: %v", err)
		}
		globalCancel()
		//genericDao.Close()
	})

	server.StartServer(app, "http.server.bind")
}

func registerShutdownHook(hook func()) {
	go func() {
		sigint := make(chan os.Signal, 1)

		// interrupt signal sent from terminal
		signal.Notify(sigint, os.Interrupt)
		// sigterm signal sent from kubernetes
		signal.Notify(sigint, syscall.SIGTERM)

		//logger.Info("OS signal '%s' received, starting shutdown", (<-sigint).String())

		hook()
	}()
}

func getGatewayUrl() (*url.URL, error) {
	return url.Parse(configloader.GetOrDefaultString("apigateway.internal.url", constants.DefaultHttpGatewayUrl))
}

// added in order to save backport compatibility with releases 6.42 and below
// initially classifier in site-management was created in wrong way
func createSiteManagementClassifier(ctx context.Context) map[string]interface{} {
	classifier := make(map[string]interface{})
	classifier["microserviceName"] = configloader.GetKoanf().MustString("microservice.name")
	classifier["scope"] = "service"
	classifier["dbClassifier"] = "default"
	classifier["namespace"] = configloader.GetKoanf().MustString(microservice_namespace)
	return classifier
}
