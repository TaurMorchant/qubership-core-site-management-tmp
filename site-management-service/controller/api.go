package controller

import (
	"bytes"
	"encoding/json"
	"github.com/gofiber/fiber/v2"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/domain"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/exceptions"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/idp"
	mdomain "github.com/netcracker/qubership-core-site-management/site-management-service/v2/paasMediationClient/domain"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/synchronizer"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/tm"
	"net/http"
	"strings"
)

const (
	IgnoreMissing   = "ignoreMissing"
	Merge           = "mergeGeneral"
	Protocol        = "protocol"
	Site            = "site"
	TenantId        = "tenantId"
	URL             = "url"
	Namespaces      = "namespaces"
	Tenant          = "Tenant"
	GenerateDefault = "generateDefaultSiteIfEmpty"
	ShowAllTenants  = "showAllTenants"
	Host            = "host"
	Async           = "async"
)

type ApiHttpHandler struct {
	Synchronizer *synchronizer.Synchronizer
	IDPClient    idp.RetryableClient
}

var logger logging.Logger

// @title Site Management API
// @description Site Management is a microservice that processes cloud project external routes.
// @contact.name Nikolay Diyakov
// @version 2.0
// @tag.name api-v1
// @tag.description Apis related to V1
// @Produce json
// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name Authorization
func init() {
	logger = logging.GetLogger("controller")
}

// HandleGetSite godoc
// @Id GetSite
// @Summary Get Site
// @Description Get Site
// @Tags api-v1
// @Produce json
// @Param tenantId path string false "tenantId"
// @Param mergeGeneral query string false "mergeGeneral"
// @Param generateDefaultSiteIfEmpty query string false "generateDefaultSiteIfEmpty"
// @Param url header string false "url"
// @Security ApiKeyAuth
// @Success 200 {string}  string
// @Failure 500 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/v1/routes/{tenantId}/site [get]
func (v *ApiHttpHandler) GetSite(c *fiber.Ctx) error {
	context := c.UserContext()

	if tenantId := c.Params(TenantId); tenantId == "" {
		logger.ErrorC(context, "Incorrect request: vars doesn't contain `tenantExternalId' key")
		return respondWithError(c, http.StatusBadRequest, "Tenant external id is not provided in request")
	} else {
		url := c.Get(URL)
		logger.Info(url)
		if url == "" {
			logger.ErrorC(context, "Error: %s", "URL is not specified")
			return respondWithError(c, http.StatusBadRequest, "URL is not specified")
		} else {
			mergeGeneral := true
			if merge := c.Query(Merge, "true"); merge == "false" {
				mergeGeneral = false
			}

			generateDefault := false
			if generate := c.Query(GenerateDefault, "false"); generate == "true" {
				generateDefault = true
			}

			if data, err := v.Synchronizer.GetSite(context, tenantId, url, mergeGeneral, generateDefault); err == nil {
				logger.DebugC(context, "Found site: %v", data)
				if len(data) > 0 {
					return respondWithString(c, http.StatusOK, data)
				} else {
					return respondWithError(c, http.StatusNotFound, "Site not found")
				}
			} else {
				logger.ErrorC(context, "Error: %s", err)
				return respondWithError(c, http.StatusInternalServerError, err.Error())
			}
		}
	}
}

// HandleGetServiceName godoc
// @Id GetServiceName
// @Summary Get Service Name
// @Description Get Service Name
// @Tags api-v1
// @Produce json
// @Param Connection header string true "Tenant"
// @Security ApiKeyAuth
// @Success 200 {string}  string
// @Failure 500 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /api/v1/tenants/current/service/name [get]
func (v *ApiHttpHandler) GetServiceName(c *fiber.Ctx) error {
	context := c.UserContext()
	tenantId := c.Get(Tenant)

	if tenantId == "" {
		logger.ErrorC(context, "Error: %s", "Tenant id is not specified")
		return respondWithError(c, http.StatusBadRequest, "Tenant id is not specified")
	}

	if data, err := v.Synchronizer.GetServiceName(context, tenantId); err == nil {
		logger.DebugC(context, "Found service name: %v", data)
		return respondWithJson(c, http.StatusOK, data)
	} else {
		logger.ErrorC(context, "Error: %s", err)
		return respondWithError(c, http.StatusInternalServerError, err.Error())
	}
}

// HandleRegisterTenant godoc
// @Id RegisterTenant
// @Summary Register Tenant
// @Description Register Tenant
// @Tags api-v1
// @Produce json
// @Param request body tm.Tenant true "tenant"
// @Security ApiKeyAuth
// @Success 201
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/tenants [post]
func (v *ApiHttpHandler) RegisterTenant(c *fiber.Ctx) error {
	context := c.UserContext()

	var tenant tm.Tenant
	if err := c.BodyParser(&tenant); err != nil {
		return respondWithError(c, http.StatusBadRequest, "Invalid request payload")
	}
	logger.InfoC(context, "Received request to register tenant: %v", tenant)

	if err := v.Synchronizer.RegisterTenant(context, tenant); err != nil {
		logger.ErrorC(context, "Error registering tenant %v: %s", tenant, err)
		return respondWithError(c, http.StatusInternalServerError, err.Error())
	}
	logger.DebugC(context, "Tenant registered: %v", tenant)
	return respondWithoutBody(c, http.StatusCreated)
}

// HandleGetRealms godoc
// @Id GetRealms
// @Summary Get Realms
// @Description Get Realms
// @Tags api-v1
// @Produce json
// @Param showAllTenants query string true "showAllTenants"
// @Security ApiKeyAuth
// @Success 200 {object} domain.Realms
// @Failure 500 {object} map[string]string
// @Router /api/v1/trusted-hosts [get]
func (v *ApiHttpHandler) GetRealms(c *fiber.Ctx) error {
	context := c.UserContext()
	showAllTenants := false
	if show := c.Query(ShowAllTenants, "false"); show == "true" {
		showAllTenants = true
	}

	if data, err := v.Synchronizer.GetRealms(context, showAllTenants); err == nil {
		logger.DebugC(context, "Found realms: %v", data)
		return respondWithJson(c, http.StatusOK, data)
	} else {
		logger.ErrorC(context, "Error: %s", err)
		return respondWithError(c, http.StatusInternalServerError, err.Error())
	}
}

// HandleGetRealm godoc
// @Id GetRealm
// @Summary Get Realm
// @Description Get Realm
// @Tags api-v1
// @Produce json
// @Param tenantId path string true "tenantId"
// @Security ApiKeyAuth
// @Success 200 {array} domain.Realm
// @Failure 500 {object} map[string]string
// @Router /api/v1/trusted-hosts/{tenantId} [get]
func (v *ApiHttpHandler) GetRealm(c *fiber.Ctx) error {
	context := c.UserContext()
	realm := c.Params(TenantId)

	if data, err := v.Synchronizer.GetRealm(context, realm); err == nil {
		logger.DebugC(context, "Found realms: %v", data)
		return respondWithJson(c, http.StatusOK, data)
	} else {
		logger.ErrorC(context, "Error: %s", err)
		return respondWithError(c, http.StatusInternalServerError, err.Error())
	}
}

// HandleGetAll godoc
// @Id GetDomainTenantDns
// @Summary Get Domain Tenant Dns
// @Description Get Domain Tenant Dns
// @Tags api-v1
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {array} domain.TenantDns
// @Failure 404 {object} map[string]string
// @Router /api/v1/routes [get]
func (v *ApiHttpHandler) GetAll(c *fiber.Ctx) error {
	context := c.UserContext()
	mergeGeneral := true
	if merge := c.Query(Merge, "true"); merge == "false" {
		mergeGeneral = false
	}
	if data, err := v.Synchronizer.FindAllWithGeneral(context, mergeGeneral); err == nil {
		// todo when migration to postgres will be done, and addresses format will be fixed during evolution, delete the line below
		for _, tenantDns := range *data {
			tenantDns.FlattenAddressesToHosts()
		}
		logger.DebugC(context, "For all tenants found routes: %s", data)
		return respondWithJson(c, http.StatusOK, data)
	} else {
		logger.DebugC(context, "No data found")
		return respondWithError(c, http.StatusNotFound, err.Error())
	}
}

// HandleValidate godoc
// @Id Validate
// @Summary Validate Scheme
// @Description Validate Scheme
// @Tags api-v1
// @Produce json
// @Param request body domain.TenantDns true "TenantDns"
// @Security ApiKeyAuth
// @Success 200 {object} domain.ValidationResult
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/validate [post]
func (v *ApiHttpHandler) Validate(c *fiber.Ctx) error {
	context := c.UserContext()

	var data domain.TenantDns
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return respondWithError(c, http.StatusBadRequest, "Invalid request payload")
	}

	logger.DebugC(context, "Check endpoints: %s", data)
	if result, err := v.Synchronizer.CheckCollisions(context, data); err != nil {
		logger.ErrorC(context, "Error perform upsert of: %s, error: %s", data, err)
		return respondWithError(c, http.StatusInternalServerError, err.Error())
	} else {
		logger.DebugC(context, "Provided Scheme is ok")
		return respondWithJson(c, http.StatusOK, result)
	}
}

// HandleGet godoc
// @Id GetTenantDns
// @Summary Get Tenant Dns
// @Description Get Tenant Dns
// @Tags api-v1
// @Produce json
// @Param tenantId path string true "tenantId"
// @Param site query string false "site"
// @Param mergeGeneral query string false "mergeGeneral" Default(true)
// @Param generateDefaultSiteIfEmpty query string false "generateDefaultSiteIfEmpty"
// @Security ApiKeyAuth
// @Success 200 {object} domain.TenantDns
// @Failure 404 {object} map[string]string
// @Router /api/v1/routes/{tenantId} [get]
func (v *ApiHttpHandler) Get(c *fiber.Ctx) error {
	context := c.UserContext()

	if tenantId := c.Params(TenantId); tenantId == "" {
		logger.ErrorC(context, "Incorrect request: vars doesn't contain `tenantId' key")
		return respondWithError(c, http.StatusBadRequest, "Tenant id is not provided in request")
	} else {
		logger.DebugC(context, "Search routes for tenant: %s", tenantId)

		tenantSite := c.Query(Site, "")
		if tenantSite != "" {
			logger.DebugC(context, "Site for tenant %s is %s", tenantId, tenantSite)
		}

		mergeGeneral := true
		if merge := c.Query(Merge, "true"); merge == "false" {
			mergeGeneral = false
		}

		generateDefault := false
		if generate := c.Query(GenerateDefault, "false"); generate == "true" {
			generateDefault = true
		}

		if data, err := v.Synchronizer.FindByTenantId(context, tenantId, tenantSite, mergeGeneral, generateDefault); err == nil {
			// todo when migration to postgres will be done, and addresses format will be fixed during evolution, delete the line below
			data.FlattenAddressesToHosts()
			logger.DebugC(context, "For tenantId: %s, found routes: %s", tenantId, data)
			return respondWithJson(c, http.StatusOK, data)
		} else if data, err := v.Synchronizer.FindByExternalTenantId(context, tenantId, tenantSite, mergeGeneral, generateDefault); err == nil {
			data.FlattenAddressesToHosts()
			logger.DebugC(context, "For tenantId: %s, found routes: %s", tenantId, data)
			return respondWithJson(c, http.StatusOK, data)
		} else {
			logger.ErrorC(context, "No data found for tenantId: %s", tenantId)
			return respondWithError(c, http.StatusNotFound, err.Error())
		}
	}
}

// HandleUpsert godoc
// @Id Upsert
// @Summary Upsert
// @Description Upsert
// @Tags api-v1
// @Produce json
// @Param request body domain.TenantDns true "TenantDns"
// @Param async query string false "async" Default(true)
// @Security ApiKeyAuth
// @Success 201 {object} domain.TenantDns
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/routes [post]
func (v *ApiHttpHandler) Upsert(c *fiber.Ctx) error {
	context := c.UserContext()
	logger.InfoC(context, "Start upsert api call")

	var data domain.TenantDns
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return respondWithError(c, http.StatusBadRequest, "Invalid request payload")
	}
	logger.DebugC(context, "Received TenantDns data: %s", &data)

	async := true

	if value := c.Query(Async, "true"); value == "false" {
		async = false
	}
	logger.Infof("value %s", async)
	logger.DebugC(context, "Flattening addresses (urls to hosts)")
	// need to convert any address with url value (with scheme and path) to host only string
	data.FlattenAddressesToHosts()

	logger.DebugC(context, "Try upsert dao call with: %s", data)
	if err := v.Synchronizer.AwaitAction(context, async, func() error { return v.Synchronizer.Upsert(context, data) }); err != nil {
		logger.ErrorC(context, "Error perform upsert of: %s. Error: %s", data, err.Error())
		return respondWithError(c, http.StatusInternalServerError, err.Error())
	}

	logger.DebugC(context, "Upsert successfully performed for: %s", data)
	return respondWithJson(c, http.StatusCreated, data)
}

// HandleUpsert godoc
// @Id UpsertUpdate
// @Summary Upsert Update
// @Description Upsert Update
// @Tags api-v1
// @Produce json
// @Param request body domain.TenantDns true "TenantDns"
// @Param async query string false "async" Default(true)
// @Security ApiKeyAuth
// @Success 201 {object} domain.TenantDns
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/routes [put]
func (v *ApiHttpHandler) UpsertUpdate(c *fiber.Ctx) error {
	return v.Upsert(c)
}

// HandleRestoreTenantAlias godoc
// @Id RestoreTenantAlias
// @Summary Restore Tenant Alias
// @Description Restore Tenant Alias
// @Tags api-v1
// @Produce json
// @Param tenantId path string true "tenantId"
// @Security ApiKeyAuth
// @Success 200
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/routes/{tenantId}/restore-tenant-alias [post]
func (v *ApiHttpHandler) RestoreTenantAlias(c *fiber.Ctx) error {
	context := c.UserContext()
	var data *domain.TenantDns
	var err error
	tenantId := c.Params(TenantId)
	if tenantId == "" {
		logger.ErrorC(context, "Incorrect request: vars doesn't contain `tenantId' key")
		return respondWithError(c, http.StatusBadRequest, "Tenant id is not provided in request")
	}
	logger.DebugC(context, "Search routes for tenant: %s", tenantId)

	data, err = v.Synchronizer.FindByTenantId(context, tenantId, "", false, false)
	if err != nil {
		data, err = v.Synchronizer.FindByExternalTenantId(context, tenantId, "", false, false)
	}
	logger.DebugC(context, "For tenantId: %s, found routes: %s", tenantId, data)
	if len(data.Sites) == 0 {
		data, err = v.Synchronizer.FindByTenantId(context, tenantId, "", false, true)
		logger.DebugC(context, "Try upsert dao call with: %s", data)
		if err := v.Synchronizer.AwaitAction(context, true, func() error { return v.Synchronizer.Upsert(context, *data) }); err != nil {
			logger.ErrorC(context, "Error perform upsert of: %s. Error: %s", data, err.Error())
			return respondWithError(c, http.StatusInternalServerError, err.Error())
		}
	}
	// todo when migration to postgres will be done, and addresses format will be fixed during evolution, delete the line below
	data.FlattenAddressesToHosts()
	err = v.Synchronizer.AwaitAction(context, true, func() error {
		return v.Synchronizer.ChangeTenantStatus(context, tenantId, true)
	})
	if err == nil {
		logger.InfoC(context, "Performed 'createRoutes' task for tenant with objectId = %s", tenantId)
		return respondWithoutBody(c, http.StatusOK)
	} else {
		return respondWithError(c, http.StatusInternalServerError, err.Error())
	}

}

// HandleDelete godoc
// @Id DeleteRoutes
// @Summary Delete Routes
// @Description Delete Routes
// @Tags api-v1
// @Produce json
// @Param tenantId path string true "tenantId"
// @Param async query string false "async" Default(true)
// @Security ApiKeyAuth
// @Success 200
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/routes/{tenantId} [delete]
func (v *ApiHttpHandler) Delete(c *fiber.Ctx) error {
	context := c.UserContext()

	if tenantId := c.Params(TenantId); tenantId == "" {
		logger.ErrorC(context, "Incorrect request: vars doesn't contain `tenantId' key")
		return respondWithError(c, http.StatusBadRequest, "Tenant id is not provided in request")
	} else {
		logger.DebugC(context, "Delete routes for tenant: %s", tenantId)
		async := true
		if value := c.Query(Async, "true"); value == "false" {
			async = false
		}

		action := func() error { return v.Synchronizer.DeleteRoutes(context, tenantId) }
		if err := v.Synchronizer.AwaitAction(context, async, action); err == nil {
			logger.DebugC(context, "Deleted routes for tenantId: %s", tenantId)
			return respondWithJson(c, http.StatusOK, "")
		} else {
			logger.DebugC(context, "Error occurred on delete routes for  tenantId: %s", tenantId)
			return respondWithError(c, http.StatusInternalServerError, err.Error())
		}
	}
}

// HandleSync godoc
// @Id Sync
// @Summary Sync By External Request
// @Description Post Sync By External Request
// @Tags api-v1
// @Produce json
// @Security ApiKeyAuth
// @Success 200
// @Router /api/v1/sync [post]
func (v *ApiHttpHandler) Sync(c *fiber.Ctx) error {
	context := c.UserContext()
	logger.InfoC(context, "Force routes sync by external request")
	v.Synchronizer.Sync(context)
	return nil
}

// HandleResetCaches godoc
// @Id ResetCaches
// @Summary ResetCaches By External Request
// @Description Post ResetCaches By External Request
// @Tags api-v1
// @Produce json
// @Security ApiKeyAuth
// @Success 200
// @Router /api/v1/reset-caches [post]
func (v *ApiHttpHandler) ResetCaches(c *fiber.Ctx) error {
	context := c.UserContext()
	logger.InfoC(context, "Force reset caches by external request")
	return respondWithJson(c, http.StatusOK, "")
}

// HandleListOpenShiftRoutes godoc
// @Id ListOpenShiftRoutes
// @Summary Requesting openshift routes
// @Description Get Requesting annotated routes bulk
// @Tags api-v1
// @Produce json
// @Security ApiKeyAuth
// @Param   collection  query     []string   false  "namespaces"  collectionFormat(multi)
// @Success 200 {array} domain.Route
// @Failure 500 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Router /api/v1/openshift-routes [get]
func (v *ApiHttpHandler) ListOpenShiftRoutes(c *fiber.Ctx) error {
	context := c.UserContext()
	logger.DebugC(context, "Requesting openshift routes")
	queryArgsMap := make(map[string][]string)
	c.Context().QueryArgs().VisitAll(func(key, value []byte) {
		queryArgsMap[string(key)] = strings.Split(string(value), ",")
	})

	if routes, err := v.Synchronizer.GetOpenShiftRoutes(context, queryArgsMap); err == nil {
		return respondWithJson(c, http.StatusOK, routes)
	} else {
		switch err.(type) {
		case exceptions.OpenShiftPermissionError:
			return respondWithError(c, http.StatusForbidden, err.Error())
		default:
			return respondWithError(c, http.StatusInternalServerError, err.Error())
		}
	}
}

// HandleListAnnotatedRoutesBulk godoc
// @Id ListAnnotatedRoutesBulk
// @Summary Requesting annotated routes bulk
// @Description Get Requesting annotated routes bulk
// @Tags api-v1
// @Produce json
// @Param request body []domain.TenantData true "TenantData"
// @Security ApiKeyAuth
// @Success 200 {array} domain.TenantData
// @Failure 500 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /api/v1/annotated-routes-bulk [post]
func (v *ApiHttpHandler) ListAnnotatedRoutesBulk(c *fiber.Ctx) error {
	context := c.UserContext()
	logger.DebugC(context, "Requesting annotated routes bulk")

	var data *[]*domain.TenantData
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	if err := decoder.Decode(&data); err != nil {
		return respondWithError(c, http.StatusBadRequest, "Invalid request payload")
	}

	for _, tenantData := range *data {
		if tenantData.Site == "" {
			tenantData.Site = "default"
		}
	}

	if services, err := v.Synchronizer.GetAnnotatedRoutesBulk(context, data); err == nil {
		return respondWithJson(c, http.StatusOK, services)
	} else {
		return respondWithError(c, http.StatusInternalServerError, err.Error())
	}
}

// HandleGetIdpRoute godoc
// @Id GetIdpRoute
// @Summary Requesting annotated routes
// @Description Get Requesting annotated routes
// @Tags api-v1
// @Produce json
// @Param tenantId query string true "tenantId"
// @Param async query string false "async" Default(http)
// @Param site query string false "site" Default(default)
// @Param ignoreMissing query string false "ignoreMissing" Default(false)
// @Security ApiKeyAuth
// @Success 200 {array} domain.CustomService
// @Failure 500 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/v1/identity-provider-route [get]
func (v *ApiHttpHandler) GetIdpRoute(c *fiber.Ctx) error {
	context := c.UserContext()
	logger.DebugC(context, "Requesting annotated routes")
	if tenantIdParam := c.Query(TenantId); len(tenantIdParam) != 0 {
		tenantId := tenantIdParam

		protocol := ""
		if proto := c.Query(Async); len(proto) != 0 {
			protocol = proto
		}

		tenantSite := "default"
		if site := c.Query(Site); len(site) != 0 {
			tenantSite = site
		}

		ignoreMissing := false
		if ignore := c.Query(IgnoreMissing, "false"); ignore == "true" {
			ignoreMissing = true
		}
		logger.DebugC(context, "Get identity-provider service for tenantId: %s, protocol: %s, ignoreMissing: %t", tenantId, protocol, ignoreMissing)

		tenantData := domain.TenantData{
			TenantId:      &tenantId,
			Protocol:      protocol,
			Site:          tenantSite,
			IgnoreMissing: ignoreMissing,
		}

		if services, err := v.Synchronizer.GetIdpRouteForTenant(context, &tenantData); err == nil {
			return respondWithJson(c, http.StatusOK, services)
		} else {
			switch err.(type) {
			case exceptions.TenantNotFoundError:
				return respondWithError(c, http.StatusNotFound, err.Error())
			case exceptions.TenantIsNotActiveError:
				return respondWithError(c, http.StatusBadRequest, err.Error())
			default:
				return respondWithError(c, http.StatusInternalServerError, err.Error())
			}
		}
	} else {
		return respondWithError(c, http.StatusBadRequest, "No tenantId specified")
	}
}

// HandleListAnnotatedRoutes godoc
// @Id ListAnnotatedRoutes
// @Summary Requesting annotated routes
// @Description Get Requesting annotated routes
// @Tags api-v1
// @Produce json
// @Param tenantId query string true "tenantId"
// @Param async query string false "async" Default(default)
// @Param site query string false "site" Default(http)
// @Param ignoreMissing query string false "ignoreMissing" Default(false)
// @Security ApiKeyAuth
// @Success 200 {array} domain.CustomService
// @Failure 500 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/v1/annotated-routes [get]
// Returns list of services able being publicated via routes
func (v *ApiHttpHandler) ListAnnotatedRoutes(c *fiber.Ctx) error {
	context := c.UserContext()
	logger.DebugC(context, "Requesting annotated routes")
	if tenantIdParam := c.Query(TenantId); len(tenantIdParam) != 0 {
		tenantId := tenantIdParam

		protocol := ""
		if proto := c.Query(Async); len(proto) != 0 {
			protocol = proto
		}

		tenantSite := "default"
		if site := c.Query(Site); len(site) != 0 {
			tenantSite = site
		}

		ignoreMissing := false
		if ignore := c.Query(IgnoreMissing, "false"); ignore == "true" {
			ignoreMissing = true
		}
		logger.DebugC(context, "Get openshift services for tenantId: %s, protocol: %s, ignoreMissing: %t", tenantId, protocol, ignoreMissing)

		tenantData := domain.TenantData{
			TenantId:      &tenantId,
			Protocol:      protocol,
			Site:          tenantSite,
			IgnoreMissing: ignoreMissing,
		}

		if services, err := v.Synchronizer.GetAnnotatedRoutesForTenant(context, &tenantData); err == nil {
			return respondWithJson(c, http.StatusOK, services)
		} else {
			switch err.(type) {
			case exceptions.TenantNotFoundError:
				return respondWithError(c, http.StatusNotFound, err.Error())
			case exceptions.TenantIsNotActiveError:
				return respondWithError(c, http.StatusBadRequest, err.Error())
			default:
				return respondWithError(c, http.StatusInternalServerError, err.Error())
			}
		}
	} else {
		return respondWithError(c, http.StatusBadRequest, "No tenantId specified")
	}
}

// HandleListPublicServices godoc
// @Id ListPublicServices
// @Summary Requesting public services
// @Description Get Requesting public services
// @Tags api-v1
// @Produce json
// @Param   collection  query     []string   false  "namespaces"
// @Security ApiKeyAuth
// @Success 200 {array} domain.Service
// @Failure 500 {object} map[string]string
// @Router /api/v1/public-services [get]
// Returns list of services able being publicated via routes
func (v *ApiHttpHandler) ListPublicServices(c *fiber.Ctx) error {
	context := c.UserContext()
	logger.DebugC(context, "Requesting public services")
	namespaces := make([]string, 0)
	if value := c.Query(Namespaces); len(value) != 0 {
		namespaces = strings.Split(string(value), ",")
	}
	if services, err := v.Synchronizer.GetPublicServices(context, namespaces); err == nil {
		return respondWithJson(c, http.StatusOK, services)
	} else {
		return respondWithError(c, http.StatusInternalServerError, err.Error())
	}
}

// HandleActivateTenant godoc
// @Id ActivateTenant
// @Summary Activate Tenant
// @Description Activate Tenant
// @Tags api-v1
// @Produce json
// @Param tenantId path string true "tenantId"
// @Param async query string false "async" Default(true)
// @Security ApiKeyAuth
// @Success 200
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Failure default {string} string
// @Router /api/v1/routes/{tenantId}/activate [post]
func (v *ApiHttpHandler) ActivateTenant(c *fiber.Ctx) error {
	context := c.UserContext()
	if tenantId := c.Params(TenantId); tenantId == "" {
		logger.ErrorC(context, "Incorrect request: vars doesn't contain `tenantId' key")
		return respondWithError(c, http.StatusBadRequest, "Tenant id is not provided in request")
	} else {
		async := true
		if value := c.Query(Async, "true"); value == "false" {
			async = false
		}

		if err := v.Synchronizer.AwaitAction(context, async,
			func() error { return v.Synchronizer.ChangeTenantStatus(context, tenantId, true) }); err == nil {
			return respondWithJson(c, http.StatusOK, nil)
		} else {
			return respondWithError(c, http.StatusInternalServerError, err.Error())
		}
	}
}

// HandleDeactivateTenant godoc
// @Id DeactivateTenant
// @Summary Deactivate Tenant
// @Description Deactivate Tenant
// @Tags api-v1
// @Produce json
// @Param tenantId path string true "tenantId"
// @Param async query string false "async" Default(true)
// @Security ApiKeyAuth
// @Success 200
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/routes/{tenantId}/deactivate [post]
func (v *ApiHttpHandler) DeactivateTenant(c *fiber.Ctx) error {
	context := c.UserContext()
	if tenantId := c.Params(TenantId); tenantId == "" {
		logger.ErrorC(context, "Incorrect request: vars doesn't contain `tenantId' key")
		return respondWithError(c, http.StatusBadRequest, "Tenant id is not provided in request")
	} else {
		async := true
		if value := c.Query(Async, "true"); value == "false" {
			async = false
		}

		if err := v.Synchronizer.AwaitAction(context, async,
			func() error { return v.Synchronizer.ChangeTenantStatus(context, tenantId, false) }); err == nil {
			return respondWithJson(c, http.StatusOK, nil)
		} else {
			return respondWithError(c, http.StatusInternalServerError, err.Error())
		}
	}
}

// HandleDeleteTenant godoc
// @Id DeleteTenant
// @Summary Delete Tenant
// @Description Delete Tenant
// @Tags api-v1
// @Produce json
// @Param tenantId path string true "tenantId"
// @Param protocol query string false "protocol"
// @Security ApiKeyAuth
// @Success 200 {string}  string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Failure default {string} string
// @Router /api/v1/tenants/{tenantId} [delete]
func (v *ApiHttpHandler) DeleteTenant(c *fiber.Ctx) error {
	context := c.UserContext()
	if tenantId := c.Params(TenantId, ""); tenantId == "" {
		logger.ErrorC(context, "Incorrect request: vars doesn't contain `tenantId' key")
		return respondWithError(c, http.StatusBadRequest, "Tenant id is not provided in request")
	} else {
		async := true
		if value := c.Query(Protocol, "true"); value == "false" {
			async = false
		}

		if err := v.Synchronizer.AwaitAction(context, async,
			func() error { return v.Synchronizer.DeleteTenant(context, tenantId) }); err == nil {
			return respondWithJson(c, http.StatusOK, nil)
		} else {
			return respondWithError(c, http.StatusInternalServerError, err.Error())
		}
	}
}

// HandleCreateTenantRoute godoc
// @Id CreateTenantRoute
// @Summary Create Tenant Route
// @Description Create Tenant Route
// @Tags api-v1
// @Produce json
// @Param tenantId path string true "tenantId"
// @Security ApiKeyAuth
// @Success 200
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Failure default {string} string
// @Router /api/v1/activate/create-os-tenant-alias-routes/perform/{tenantId} [post]
func (v *ApiHttpHandler) CreateTenantRoute(c *fiber.Ctx) error {
	context := c.UserContext()
	tenantId := c.Params(TenantId, "")
	if tenantId == "" {
		logger.ErrorC(context, "Incorrect request: vars doesn't contain `tenantId' key")
		return respondWithError(c, http.StatusBadRequest, "Tenant id is not provided in request")
	}
	logger.InfoC(context, "Start create route by tenantId=%s", tenantId)
	tenantSite := ""
	data, err := v.Synchronizer.FindByTenantId(context, tenantId, tenantSite, false, false)
	if err != nil {
		logger.ErrorC(context, "No data found for tenantId=%s", tenantId)
		return respondWithError(c, http.StatusNotFound, err.Error())
	}
	logger.DebugC(context, "Found tenant = %s", data)
	data.FlattenAddressesToHosts()
	logger.InfoC(context, "Start create route with sites = %s", data.Sites)
	if data == nil || data.Sites == nil || len(data.Sites) == 0 {
		logger.InfoC(context, "The routes schema is missing on SM side")
		// the routes schema is missing on SM side
		// need to load default routes for this tenant and create them on the site-management side
		data, err = v.Synchronizer.FindByTenantId(context, tenantId, tenantSite, false, true)
		if err != nil {
			logger.ErrorC(context, "No data found for tenantId=%s", tenantId)
			return respondWithError(c, http.StatusNotFound, err.Error())
		}
		data.FlattenAddressesToHosts()
		logger.DebugC(context, "Try upsert dao call with: %s", data)
		if err := v.Synchronizer.AwaitAction(context, true, func() error { return v.Synchronizer.Upsert(context, *data) }); err != nil {
			logger.ErrorC(context, "Error perform upsert of: %s. Error: %s", data, err.Error())
			return respondWithError(c, http.StatusInternalServerError, err.Error())
		}
	}
	err = v.Synchronizer.AwaitAction(context, true, func() error {
		return v.Synchronizer.ChangeTenantStatus(context, tenantId, true)
	})
	if err == nil {
		logger.InfoC(context, "Performed 'createRoutes' task for tenant with objectId = %s", tenantId)
		return respondWithJson(c, http.StatusOK, nil)
	} else {
		return respondWithError(c, http.StatusInternalServerError, err.Error())
	}
}

// HandleSyncIDP godoc
// @Id SyncIDP
// @Summary Sync IDP
// @Description Sync IDP
// @Tags api-v1
// @Produce json
// @Security ApiKeyAuth
// @Success 200
// @Failure 500 {object} map[string]string
// @Router /api/v1/routes/sync-idp [post]
func (v *ApiHttpHandler) SyncIDP(c *fiber.Ctx) error {
	ctx := c.UserContext()
	v.IDPClient.Reset()
	if err := v.Synchronizer.SendRoutesToIDP(ctx); err != nil {
		return respondWithError(c, http.StatusInternalServerError, err.Error())
	} else {
		return respondWithoutBody(c, http.StatusOK)
	}
}

// HandleSearch godoc
// @Id Search
// @Summary Search
// @Description Search
// @Tags api-v1
// @Produce json
// @Param host query string true "host"
// @Security ApiKeyAuth
// @Success 200 {array} domain.TenantDns
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/search [get]
func (v *ApiHttpHandler) Search(c *fiber.Ctx) error {
	ctx := c.UserContext()
	if host := c.Query(Host); len(host) != 0 {
		address := domain.Address(host)
		host := address.Host()
		if host == "" {
			return respondWithError(c, http.StatusBadRequest, "Host format is not valid")
		}
		if data, err := v.Synchronizer.FindAll(ctx); err == nil {
			logger.DebugC(ctx, "For all tenants found routes: %s", data)
			result := make([]domain.TenantDns, 0)
			for _, tenant := range *data {
				for _, services := range tenant.Sites {
					for _, addresses := range services {
						for _, address := range addresses {
							if strings.ToLower(address.Host()) == strings.ToLower(host) {
								result = append(result, tenant)
							}
						}
					}
				}
			}
			return respondWithJson(c, http.StatusOK, result)
		} else {
			logger.ErrorC(ctx, "Error getting data: %v", err.Error())
			return respondWithError(c, http.StatusInternalServerError, err.Error())
		}
	} else {
		return respondWithError(c, http.StatusBadRequest, "No host specified")
	}
}

// HandleGetTenantCurrentServices godoc
// @Id GetTenantCurrentServices
// @Summary Get tenant current services
// @Description Get tenant current services
// @Tags api-v1
// @Produce json
// @Param X-Forwarded-Proto header string false "X-Forwarded-Proto"
// @Param Tenant header string true "Tenant"
// @Security ApiKeyAuth
// @Success 200 {array}  domain.CustomService
// @Router /api/v1/tenants/current/services [get]
func (v *ApiHttpHandler) GetTenantCurrentServices(ctx *fiber.Ctx) error {
	var scheme *domain.TenantDns
	var err error
	logger.InfoC(ctx.UserContext(), "Get tenant current services")

	forwardedProto := strings.Split(strings.ToLower(ctx.Get("X-Forwarded-Proto", "")), ",")[0]
	logger.DebugC(ctx.UserContext(), "X-Forwarded-Proto = %s", forwardedProto)

	externalId := ctx.Get(Tenant)
	if externalId != "" {
		logger.DebugC(ctx.UserContext(), "Get urls for tenant with id = %s", externalId)
		scheme, err = v.Synchronizer.FindByExternalTenantId(ctx.UserContext(), externalId, "", false, false)
		logger.DebugC(ctx.UserContext(), "tenant from tenant-manager = %v", scheme)
		if err != nil {
			return err
		}
	} else {
		logger.WarnC(ctx.UserContext(), "Empty tenantId")
		scheme = &domain.TenantDns{TenantId: ""}
	}

	tenantData := domain.TenantData{
		TenantId:      &scheme.TenantId,
		Protocol:      forwardedProto,
		Site:          "default",
		IgnoreMissing: true,
	}

	var services *[]mdomain.CustomService
	if *tenantData.TenantId != "" {
		services, err = v.Synchronizer.GetAnnotatedRoutesForTenant(ctx.UserContext(), &tenantData)
	} else {
		services, err = v.Synchronizer.GetAnnotatedRoutes(ctx.UserContext(), &tenantData, scheme)
	}

	if err != nil {
		return err
	}
	return respondWithJson(ctx, http.StatusOK, services)
}

func respondWithError(c *fiber.Ctx, code int, msg string) error {
	return respondWithJson(c, code, map[string]string{"error": msg})
}

func respondWithJson(c *fiber.Ctx, code int, payload interface{}) error {
	return c.Status(code).JSON(payload)
}

func respondWithoutBody(c *fiber.Ctx, code int) error {
	return c.Status(code).JSON("")
}

func respondWithString(c *fiber.Ctx, code int, response string) error {
	return c.Status(code).SendString(response)
}
