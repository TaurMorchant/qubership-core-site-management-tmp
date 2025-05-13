package synchronizer

import (
	"context"
	"encoding/json"
	"fmt"
	pgdbaas "github.com/netcracker/qubership-core-lib-go-dbaas-postgres-client/v4"
	"github.com/netcracker/qubership-core-lib-go/v3/configloader"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-lib-go/v3/serviceloader"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/composite"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/dao/pg"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/domain"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/domain/validator"
	wrappers "github.com/netcracker/qubership-core-site-management/site-management-service/v2/domain/wrappers"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/exceptions"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/http/websocket"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/messaging"
	pmClient "github.com/netcracker/qubership-core-site-management/site-management-service/v2/paasMediationClient"
	mdomain "github.com/netcracker/qubership-core-site-management/site-management-service/v2/paasMediationClient/domain"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/tm"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/utils"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-errors/errors"
	"github.com/valyala/fasthttp"
)

const (
	timeout              = 180000
	serviceTimeout       = 20000
	step                 = 2000
	nameMaxLength        = 63
	shoppingFrontendName = "shopping-frontend"
	defaultNamespace     = "default"
	dmp                  = "dmp"
	tenantPrefix         = "tenant-"
)

type (
	syncEvent struct{}

	RoutesGetter   func(ctx context.Context, namespace string, filter func(*mdomain.Route) bool) (*[]mdomain.Route, error)
	ServicesGetter func(ctx context.Context, namespace string, filter func(*mdomain.Service) bool) (*[]mdomain.Service, error)

	Synchronizer struct {
		dao                    *pg.RouteManagerDao
		pmClient               *pmClient.PaasMediationClient
		mailSender             *messaging.MailSender
		autoRefreshTimeout     time.Duration
		bus                    chan syncEvent
		schemeValidator        validator.SchemeValidator
		routesGetter           RoutesGetter
		servicesGetter         ServicesGetter
		defaultDomainZone      string
		tenantClient           *tm.Client
		platformHostname       string
		protocol               string
		idpFacade              IDPFacade
		compositeSatelliteMode bool
		baselineSM             *composite.BaselineSM
	}
)

type Admin struct {
	Email string `json:"login"`
}

type Tenant struct {
	ObjectId    string `json:"objectId"`
	ExternalId  string `json:"externalId"`
	DomainName  string `json:"domainName"`
	ServiceName string `json:"serviceName"`
	TenantName  string `json:"name"`
	Namespace   string `json:"namespace"`
	User        Admin  `json:"admin"`
}

var logger logging.Logger

func init() {
	logger = logging.GetLogger("synchronizer")
	logger.Info("synchronizer logger was initiated")
}

// actualizeTenantStatus actualizes tenant status if necessary.
// This is a hack for Composite Platform in case SM in Composite Platform satellites
// don't get notified about tenant status changes.
func (s *Synchronizer) actualizeTenantStatus(ctx context.Context, tenant *domain.TenantDns, active bool) error {
	if tenant.Active != active {
		tenant.Active = active
		if err := s.ChangeTenantStatus(ctx, tenant.TenantId, active); err != nil {
			logger.ErrorC(ctx, "Failed to update tenant %+v status to active=%v", *tenant, active)
			return err
		}
	}
	return nil
}

func (s *Synchronizer) GetServiceName(ctx context.Context, externalId string) (string, error) {
	logger.InfoC(ctx, "Get service name for tenant with external id %s", externalId)
	tenantData, err := s.tenantClient.GetTenantByExternalId(ctx, externalId)
	if err != nil {
		return "", err
	}

	tenant, err := s.dao.FindByTenantId(ctx, tenantData.ObjectId)
	if err != nil {
		return "", err
	}

	if err := s.actualizeTenantStatus(ctx, tenant, tenantData.Status == domain.Active); err != nil {
		return "", err
	}

	if tenant.ServiceName == "" {
		serviceName, err := s.generateUniqueServiceName(ctx, tenant.TenantName)
		if err != nil {
			return "", err
		}
		tenant.ServiceName = serviceName
		if err := s.dao.Upsert(ctx, *tenant); err != nil {
			return "", err
		}
		logger.InfoC(ctx, "Generated ServiceName: %s", serviceName)
	}

	return tenant.ServiceName, nil
}

func (s *Synchronizer) GetSite(ctx context.Context, externalId string, url string, mergeGeneral bool, generateDefault bool) (string, error) {
	logger.DebugC(ctx, "Get site for tenant with external id %s and url %s", externalId, url)
	tenant, err := s.FindByExternalTenantId(ctx, externalId, "", mergeGeneral, generateDefault)
	if err != nil {
		return "", err
	} else {
		return extractSiteByHost(tenant.Sites, url), nil
	}
}

func extractSiteByHost(sites domain.Sites, url string) string {
	foundSites := make(map[string]bool)
	for site := range sites {
		services := sites[site]
		for service := range services {
			addressList := services[service]
			for i := range addressList {
				address := addressList[i]
				if address.Host() == url {
					foundSites[site] = true
				}
			}
		}
	}
	if len(foundSites) > 0 {
		if foundSites[defaultNamespace] {
			return defaultNamespace
		} else {
			for foundSite := range foundSites {
				return foundSite
			}
		}
	}

	return ""
}

func (s *Synchronizer) GetRealms(ctx context.Context, showAll bool) (*domain.Realms, error) {
	logger.InfoC(ctx, "Get realms with showAll property = %v", showAll)
	commonRoutes, realms, err := s.getRealms(ctx)
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while building realms : %s", err.Error())
		return nil, err
	}

	return &domain.Realms{
		CommonRoutes: commonRoutes,
		Tenants:      realms,
	}, nil
}

func (s *Synchronizer) SendRoutesToIDP(ctx context.Context) error {
	commonURIs, tenantURIs, err := s.getRealms(ctx)
	if err != nil {
		return errors.Wrap(err, 0)
	}
	tenantRoutes := make(map[string][]string)
	for _, realm := range tenantURIs {
		tenantRoutes[realm.RealmId] = realm.Routes
	}
	err = s.idpFacade.SetRedirectURIs(ctx, tenantRoutes, commonURIs)
	if err != nil {
		return errors.Wrap(err, 0)
	}
	return nil
}

func (s *Synchronizer) getRealms(ctx context.Context) ([]string, []domain.Realm, error) {
	logger.InfoC(ctx, "Start building realms by getting openshift routes from master namespace")
	routes, err := s.pmClient.GetRoutes(ctx, s.pmClient.Namespace) // get all routes
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while getting openshift routes : %s", err.Error())
		return nil, nil, err
	}

	logger.InfoC(ctx, "Build realms from %d openshift routes", len(*routes))
	commonRoutes, tenantsRoutes := getTenantRoutesFromOpenshiftRoutes(ctx, routes)

	commonRoutes, err = s.appendCommonExternalRoutes(ctx, commonRoutes)
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while getting common external routes: %s", err.Error())
		return nil, nil, err
	}
	err = s.appendTenantExternalRoutes(ctx, &tenantsRoutes)
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while getting tenants external routes: %s", err.Error())
		return nil, nil, err
	}
	// need to iterate over each active tenant and populate the list of routes for each tenant
	tenants := s.tenantClient.GetActiveTenantsCache(ctx)
	logger.InfoC(ctx, "Was received %d activated tenants", len(tenants))
	resultRealms := []domain.Realm{}
	for _, tenant := range tenants {
		if tenant.ExternalId == "" {
			// active tenant has empty externalId
			logger.ErrorC(ctx, "Active tenant with objectId %s has empty externalId. Do not add routes for this tenant to the result", tenant.ObjectId)
			continue
		}
		var tenantRoutes []string
		if tenantHosts, ok := tenantsRoutes[tenant.ObjectId]; ok {
			// there are tenant specific route(s) for this tenant
			tenantRoutes = tenantHosts
		} else {
			// no tenant specific routes, return empty list
			tenantRoutes = []string{}
		}
		resultRealms = append(
			resultRealms,
			domain.Realm{
				RealmId: tenant.ExternalId,
				Routes:  tenantRoutes,
			})
	}
	logger.InfoC(ctx, "Return %d common routes and %d realms", len(commonRoutes), len(resultRealms))
	return commonRoutes, resultRealms, nil
}

func (s *Synchronizer) appendCommonExternalRoutes(ctx context.Context, commonRoutes []string) ([]string, error) {
	logger.InfoC(ctx, "Start appending common external routes by getting configmaps from namespace")
	configmaps, err := s.pmClient.GetConfigMaps2(ctx, s.pmClient.Namespace, func(configmap *mdomain.Configmap) bool {
		if configmap.Metadata.Name == pmClient.TMConfigsConfigmapName {
			return true
		}
		return false
	})
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while getting configmaps : %s", err.Error())
		return nil, err
	}

	if len(*configmaps) > 0 {
		logger.InfoC(ctx, "Found configmap with external routes")
		var externalRoutes []string
		configmapData := (*configmaps)[0].Data.ExternalRoutes
		if len(configmapData) > 0 {
			logger.InfoC(ctx, "Configmap with external routes is not empty. Add external routes from configmap to common routes")
			err = json.Unmarshal([]byte(configmapData), &externalRoutes)
			if err != nil {
				logger.ErrorC(ctx, "Error occurred while unmarshalling external routes from configmap: %s", err.Error())
				return nil, err
			}
			if len(externalRoutes) > 0 {
				for _, externalRoute := range externalRoutes {
					commonRoutes = append(commonRoutes, externalRoute)
				}
			}
		} else {
			logger.InfoC(ctx, "Configmap with external routes is empty")
		}
	}
	logger.InfoC(ctx, "Return %s common routes", commonRoutes)
	return commonRoutes, nil
}

func (s *Synchronizer) appendTenantExternalRoutes(ctx context.Context, tenantRoutes *map[string][]string) error {
	logger.InfoC(ctx, "Start appending tenant external routes")
	for tenantObjectId, routes := range *tenantRoutes {
		tenant, err := s.tenantClient.GetTenantByObjectId(ctx, tenantObjectId)
		if err != nil {
			if err == tm.ErrTenantNotFound {
				logger.DebugC(ctx, "Tenant with objectId %s was not found in tenant manager. Skip searching for external routes", tenantObjectId)
				continue
			}
			logger.ErrorC(ctx, "Error occurred while getting tenant %s from tenant manager: %s", tenantObjectId, err.Error())
			return err
		}

		namespaces := strings.Split(tenant.Namespace, ",")

		for _, namespace := range namespaces {
			if namespace != "" {
				logger.InfoC(ctx, "Tenant %s has namespaces different from master", tenantObjectId)
				namespaceRoutes, err := s.pmClient.GetRoutes(ctx, namespace)
				if err != nil {
					logger.ErrorC(ctx, "Error occurred while getting routes from namespace %s: %s", namespace, err.Error())
					return err
				}
				for _, namespaceRoute := range *namespaceRoutes {
					if namespaceRoute.GetTenantId() == tenantObjectId || namespaceRoute.GetTenantId() == "GENERAL" {
						appendElemIfNotExists(&routes, namespaceRoute.Spec.Host)
						(*tenantRoutes)[tenantObjectId] = routes
					}
				}
			}
		}
	}
	return nil
}

func (s *Synchronizer) GetRealm(ctx context.Context, realmId string) (*domain.Realm, error) {
	logger.InfoC(ctx, "Get hosts for realm %s", realmId)
	scheme, err := s.dao.FindByTenantId(ctx, realmId)
	if err != nil {
		return nil, err
	}

	var routes []string
	for _, siteServices := range scheme.Sites {
		for _, addresses := range siteServices {
			for _, address := range addresses {
				routes = append(routes, address.Host())
			}
		}
	}
	logger.InfoC(ctx, "Got %d hosts from database", len(routes))

	namespaces := scheme.Namespaces
	appendElemIfNotExists(&namespaces, s.pmClient.Namespace)

	for _, namespace := range namespaces {
		generalRoutes, err := s.pmClient.GetRoutesByFilter(ctx, namespace, RouteIsGeneral)
		if err != nil {
			return nil, err
		}

		for _, route := range *generalRoutes {
			routes = append(routes, route.Spec.Host)
		}
	}

	logger.InfoC(ctx, "Return %d result hosts", len(routes))
	return &domain.Realm{
		Routes: routes,
	}, nil
}

func (s *Synchronizer) FindAll(ctx context.Context) (*[]domain.TenantDns, error) {
	return s.FindAllWithGeneral(ctx, true)
}

func (s *Synchronizer) FindAllWithGeneral(ctx context.Context, mergeGeneral bool) (*[]domain.TenantDns, error) {
	if scheme, err := s.dao.FindAll(ctx); err == nil {
		logger.DebugC(ctx, "Find all result: %v", scheme)
		namespaces := s.getAllNamespacesFromTenants(scheme)
		appendElemIfNotExists(&namespaces, s.pmClient.Namespace)

		if generalRoutes, err := s.pmClient.GetRoutesForNamespaces2(ctx, namespaces, RouteIsGeneral); err != nil {
			return nil, err
		} else if mergeGeneral {
			logger.DebugC(ctx, "General routes: %v", generalRoutes)
			for index, tenant := range *scheme {
				(*scheme)[index] = *(domain.MergeDatabaseSchemeWithGeneralRoutes(&tenant, generalRoutes))
			}
		}

		return scheme, nil

	} else {
		return nil, err
	}
}

func (s *Synchronizer) getAllNamespacesFromTenants(tenants *[]domain.TenantDns) []string {
	namespaces := make([]string, 0)
	if tenants != nil {
		for _, tenant := range *tenants {
			for _, namespace := range tenant.Namespaces {
				if namespace != "" {
					appendElemIfNotExists(&namespaces, namespace)
				}
			}
		}
	}
	appendElemIfNotExists(&namespaces, s.pmClient.Namespace)
	return namespaces
}

func (s *Synchronizer) FindByExternalTenantId(ctx context.Context, externalId, tenantSite string, mergeGeneral, generateDefault bool) (*domain.TenantDns, error) {
	logger.DebugC(ctx, "Get tenant for external id %s", externalId)
	tenantData, err := s.tenantClient.GetTenantByExternalId(ctx, externalId)
	if err != nil {
		return nil, err
	}
	tenant, err := s.FindByTenantId(ctx, tenantData.ObjectId, tenantSite, mergeGeneral, generateDefault)
	if err != nil {
		return nil, err
	}
	if err := s.actualizeTenantStatus(ctx, tenant, tenantData.Status == domain.Active); err != nil {
		return tenant, err
	}
	return tenant, nil
}

func (s *Synchronizer) FindByTenantId(ctx context.Context, tenantId, tenantSite string, mergeGeneral, generateDefault bool) (*domain.TenantDns, error) {
	if scheme, err := s.dao.FindByTenantId(ctx, tenantId); err == nil {
		if tenantSite != "" {
			scheme = domain.FilterBySite(scheme, tenantSite)
		}
		logger.DebugC(ctx, "Scheme filtered by site: %v", scheme)

		logger.InfoC(ctx, "sites: %v, generateDefault: %v", scheme.Sites, generateDefault)
		if len(scheme.Sites) == 0 && generateDefault {
			if tenantSite == "" {
				tenantSite = "default"
			}
			if scheme.TenantName == "" {
				tenant, err := s.tenantClient.GetTenantByObjectId(ctx, tenantId)
				if err != nil {
					return nil, err
				}
				scheme.TenantName = tenant.TenantName
			}
			tenantData := domain.TenantData{
				TenantId:      &tenantId,
				Protocol:      s.protocol,
				Site:          tenantSite,
				TenantName:    scheme.TenantName,
				IgnoreMissing: true,
			}

			routes, err := s.GetAnnotatedRoutesForTenant(ctx, &tenantData)
			if err != nil {
				return nil, err
			}

			services := domain.Services{}
			sites := domain.Sites{}
			sites[tenantSite] = services

			for _, route := range *routes {
				address, err := url.Parse(route.URL)
				if err != nil {
					return nil, err
				}
				services[route.Id] = domain.AddressList{domain.Address(address.Host)}
			}
			scheme.Sites = sites
		} else if mergeGeneral {
			namespaces := scheme.Namespaces
			appendElemIfNotExists(&namespaces, s.pmClient.Namespace)
			if generalRoutes, err := s.pmClient.GetRoutesForNamespaces2(ctx, namespaces, RouteIsGeneral); err == nil {
				logger.DebugC(ctx, "General routes: %v", generalRoutes)
				scheme = domain.MergeDatabaseSchemeWithGeneralRoutes(scheme, generalRoutes)
			} else {
				return nil, err
			}
		}
		return scheme, nil
	} else {
		return nil, err
	}
}

func (s *Synchronizer) GetOpenShiftRoutes(ctx context.Context, params map[string][]string) (*[]mdomain.Route, error) {
	if namespace, ok := params["namespace"]; ok && len(namespace[0]) != 0 {
		logger.DebugC(ctx, "Get openshift routes for namespace %s", namespace[0])
		return s.getOpenShiftRoutesWithNamespace(ctx, []string{namespace[0]})
	} else if name, ok := params["name"]; ok && len(name[0]) != 0 {
		namespaces, ok := params["namespaces"]
		if !ok {
			namespaces = []string{s.pmClient.Namespace}
		}
		logger.DebugC(ctx, "Get openshift route with name %s and namespaces: %v", name[0], namespaces)
		return s.getOpenShiftRoutesWithName(ctx, name[0], namespaces)
	} else if namespaces, ok := params["namespaces"]; ok && len(namespaces[0]) != 0 {
		logger.DebugC(ctx, "Get openshift routes for namespaces %s", namespaces[0])
		return s.getOpenShiftRoutesWithNamespace(ctx, strings.Split(namespaces[0], ","))
	} else {
		logger.DebugC(ctx, "Get openshift routes for default namespace")
		return s.pmClient.GetRoutes(ctx, s.pmClient.Namespace)
	}
}

func (s *Synchronizer) getOpenShiftRoutesWithName(ctx context.Context, name string, namespaces []string) (*[]mdomain.Route, error) {
	filter := func(route *mdomain.Route) bool {
		return route.Metadata.Name == name
	}
	return s.pmClient.GetRoutesForNamespaces2(ctx, namespaces, filter)
}

func (s *Synchronizer) getOpenShiftRoutesWithNamespace(ctx context.Context, namespaces []string) (*[]mdomain.Route, error) {
	return s.pmClient.GetRoutesForNamespaces(ctx, namespaces)
}

func (s *Synchronizer) GetAnnotatedRoutesBulk(ctx context.Context, data *[]*domain.TenantData) (*[]*domain.TenantData, error) {
	logger.InfoC(ctx, "Get bulk annotated routes")
	allTenants, err := s.dao.FindAll(ctx)
	if err != nil {
		return nil, err
	}

	for _, tenantData := range *data {
		var tenantScheme domain.TenantDns

		for _, tenant := range *allTenants {
			if tenantData.TenantId != nil && tenant.TenantId == *tenantData.TenantId {
				tenantScheme = tenant
			}
		}

		if tenantScheme.TenantId != "" && tenantScheme.Active || tenantData.IgnoreMissing {
			logger.DebugC(ctx, "Tenant '%s' is in active state or ignoreMissing is true", tenantScheme.TenantId)
			routes, err := s.GetAnnotatedRoutes(ctx, tenantData, &tenantScheme)
			if err != nil {
				logger.ErrorC(ctx, "Error occurred while getting annotated routes for tenant '%s': %s", tenantData.TenantId, err.Error())
			} else {
				tenantData.Routes = *routes
			}
		} else {
			logger.DebugC(ctx, "Tenant '%s' is not in active state and ignore missing parameter is false")
		}
	}
	return data, nil
}

// GetIdpRouteForTenant finds identity-provider route whether in routes of current namespace
// or by getting it from baseline SM. Function returns slice with single element since REST API contract requires slice.
func (s *Synchronizer) GetIdpRouteForTenant(ctx context.Context, data *domain.TenantData) ([]mdomain.CustomService, error) {
	// get local routes since it might contain custom route for identity-provider
	routes, err := s.GetAnnotatedRoutesForTenant(ctx, data)
	if err != nil {
		return nil, err
	}
	return s.getIdentityProviderRoute(ctx, data, *routes)
}

func (s *Synchronizer) GetAnnotatedRoutesForTenant(ctx context.Context, data *domain.TenantData) (*[]mdomain.CustomService, error) {
	scheme, err := s.dao.FindByTenantId(ctx, *data.TenantId)
	var tenantData *tm.Tenant

	if err != nil {
		logger.InfoC(ctx, "Empty result or search error: %s. Try to find by externalId.", err)
		tenantData, err = s.tenantClient.GetTenantByExternalId(ctx, *data.TenantId)
		if err == nil {
			scheme, err = s.dao.FindByTenantId(ctx, tenantData.ObjectId)
			if err := s.actualizeTenantStatus(ctx, scheme, tenantData.Status == domain.Active); err != nil {
				return nil, err
			}
		} else {
			tenantData = &tm.Tenant{}
		}
	}

	if err != nil || !scheme.Active {
		logger.InfoC(ctx, "Tenant %s is not active or missing in database, ignoreMissing parameter is %t", *data.TenantId, data.IgnoreMissing)
		if data.IgnoreMissing {
			if err != nil {
				scheme = &domain.TenantDns{
					TenantId:   *data.TenantId,
					TenantName: tenantData.TenantName,
				}
			}
		} else {
			if err == nil {
				err = exceptions.NewTenantIsNotActiveError(*data.TenantId)
			}
			return nil, err
		}
	}

	return s.GetAnnotatedRoutes(ctx, data, scheme)
}

func (s *Synchronizer) GetAnnotatedRoutes(ctx context.Context, data *domain.TenantData, scheme *domain.TenantDns) (*[]mdomain.CustomService, error) {
	if data.TenantId != nil {
		logger.InfoC(ctx, "Getting annotated routes for tenant with id:%s and name:%s", *data.TenantId, scheme.TenantName)
	}
	logger.InfoC(ctx, "Protocol: '%s', site: '%s', ignoreMissing: '%v'", data.Protocol, data.Site, data.IgnoreMissing)

	if !scheme.Active {
		if err := s.mergeDatabaseRoutesWithGenerated(ctx, scheme, data.Site); err != nil {
			return nil, err
		}
	}

	scheme = domain.FilterBySite(scheme, data.Site)

	if scheme.ServiceName != "" {
		delete(scheme.Sites[data.Site], scheme.ServiceName)
	}

	namespaces := scheme.Namespaces
	appendElemIfNotExists(&namespaces, s.pmClient.Namespace)
	routes, err := s.pmClient.GetRoutesForNamespaces2(ctx, namespaces, RouteIsGeneral)
	if err != nil {
		return nil, err
	}
	routes = scheme.AppendToRoutes(routes)
	return s.buildCustomServicesFromRoutes(ctx, routes, data.Protocol, namespaces)
}

func (s *Synchronizer) getIdentityProviderRoute(ctx context.Context, data *domain.TenantData, services []mdomain.CustomService) ([]mdomain.CustomService, error) {
	pgwUrl := ""
	for _, service := range services {
		if service.Id == domain.PublicGatewayServiceId {
			pgwUrl = service.URL
			break
		}
	}
	if s.compositeSatelliteMode {
		baselineIdpRoute, err := s.baselineSM.GetIdpRoute(ctx, *data.TenantId, data.Protocol, data.Site, data.IgnoreMissing)
		if err != nil {
			logger.ErrorC(ctx, "Could not load baseline idp route: %v", err)
			return nil, errors.New("could not load baseline idp route: " + err.Error())
		}
		logger.DebugC(ctx, "Identity-provider url obtained from baseline SM: %+v", baselineIdpRoute)
		return baselineIdpRoute, err
	} else { // this is baseline so by default SM should return public gateway url as IDP url
		defaultIdpRoute := mdomain.CustomService{
			Id:          domain.IdentityProviderId,
			Name:        "Identity Provider",
			URL:         pgwUrl,
			Description: "URL to access Identity Provider API",
		}
		logger.DebugC(ctx, "This is a baseline so default identity-provider url (public-gateway url) will be returned: %+v", defaultIdpRoute)
		return []mdomain.CustomService{defaultIdpRoute}, nil
	}
}

func (s *Synchronizer) mergeDatabaseRoutesWithGenerated(ctx context.Context, scheme *domain.TenantDns, site string) error {
	services, err := s.generateDefaultRoutes(ctx, scheme.DomainName, scheme.TenantName, scheme.Namespaces)
	if err != nil {
		return err
	}
	if scheme.Sites == nil {
		scheme.Sites = make(map[string]domain.Services)
	}

	if _, ok := scheme.Sites[site]; !ok {
		logger.DebugC(ctx, "There is no scheme for site '%s', insert whole generated scheme", site)
		scheme.Sites[site] = services
	} else {
		logger.DebugC(ctx, "There are configured services for site '%s', add only missed services", site)
		for service, addressList := range services {
			if _, ok := scheme.Sites[site][service]; !ok {
				logger.DebugC(ctx, "Service '%s' was not found in scheme and will be added with generated url", service)
				scheme.Sites[site][service] = addressList
			}
		}
	}
	logger.DebugC(ctx, "Scheme with default routes : %v", scheme.Sites[site])
	return nil
}

func (s *Synchronizer) generateDefaultRoutes(ctx context.Context, domainName, tenantName string, namespaces []string) (domain.Services, error) {
	logger.InfoC(ctx, "Generate default routes for tenant with domainName '%s' and tenant name '%s'", domainName, tenantName)
	appendElemIfNotExists(&namespaces, s.pmClient.Namespace)
	publicServices, err := s.GetPublicServices(ctx, namespaces)
	if err != nil {
		return nil, err
	}
	result, err := s.generateRoutesForServices(ctx, domainName, publicServices, namespaces)
	if err != nil {
		return nil, err
	}

	logger.InfoC(ctx, "Default routes were generated successfully")
	return result, nil
}

// TODO: add templates like in tenant manager
func (s *Synchronizer) generateRoutesForServices(ctx context.Context, domainName string, publicServices *[]mdomain.Service, namespaces []string) (domain.Services, error) {
	logger.DebugC(ctx, "Build url for public services %v", publicServices)
	result := make(map[string]domain.AddressList)

	for _, service := range *publicServices {
		logger.DebugC(ctx, "Build url for service '%s'", service.Metadata.Name)
		var urlForService url.URL

		if domainName != "" {
			prefix := service.GetPrefix()
			host := prefix + "." + domainName
			scheme := s.protocol

			urlForService = url.URL{
				Host:   host,
				Path:   "/",
				Scheme: scheme,
			}
			logger.DebugC(ctx, "Domain name = '%s', url for service '%s': %s", domainName, service.Metadata.Name, urlForService.String())
		} else {
			routes, err := s.pmClient.GetRoutesForNamespaces2(ctx, namespaces, func(route *mdomain.Route) bool {
				return RouteIsGeneral(route) && route.Spec.Service.Name == service.Metadata.Name
			})
			logger.DebugC(ctx, "Domain name is empty, get url from route: %v", routes)
			if err != nil {
				return nil, err
			}

			if len(*routes) > 0 {
				urlForService = url.URL{
					Host:   (*routes)[0].Spec.Host,
					Path:   "/",
					Scheme: s.protocol,
				}
			}
		}
		if urlForService.Host != "" {
			logger.DebugC(ctx, "For service '%s' generated url is '%s'", service.Metadata.Name, urlForService.String())
			result[service.Metadata.Name] = domain.AddressList{domain.Address(urlForService.String())}
		} else {
			logger.ErrorC(ctx, "Url for service '%s' cannot be generated", service)
			result[service.Metadata.Name] = domain.AddressList{""}
		}
	}
	return result, nil
}

func (s *Synchronizer) generateShoppingRoute(ctx context.Context, domainName, tenantName string) domain.AddressList {
	host := ""
	if domainName != "" {
		host = domainName
	} else if s.defaultDomainZone != "" {
		host = tenantName + "." + s.defaultDomainZone
	} else {
		host = tenantName + "." + s.platformHostname
	}
	scheme := s.protocol
	shoppingUrl := url.URL{
		Host:   host,
		Path:   "/",
		Scheme: scheme,
	}
	logger.DebugC(ctx, "For service '%s' generated url is '%s'", "shopping-frontend", shoppingUrl.String())
	return domain.AddressList{domain.Address(shoppingUrl.String())}
}

// Returns services marked by annotation key "qubership.cloud/tenant.service.alias.prefix" as public
func (s *Synchronizer) GetPublicServices(ctx context.Context, namespaces []string) (*[]mdomain.Service, error) {
	filter := func(service *mdomain.Service) bool {
		_, ok := serviceloader.MustLoad[utils.AnnotationMapper]().Get(service.Metadata.Annotations, "tenant.service.alias.prefix")
		return ok
	}

	result := make([]mdomain.Service, 0)
	if len(namespaces) == 0 {
		namespaces = append(namespaces, s.pmClient.Namespace)
	}
	for _, namespace := range namespaces {
		raw, err := s.pmClient.GetServices2(ctx, namespace, filter)
		if err != nil {
			return nil, errors.New(fmt.Sprintf("Error get service list: %v", err))
		}
		result = append(result, *raw...)
	}

	return &result, nil
}

func (s *Synchronizer) AwaitAction(ctx context.Context, async bool, function func() error) error {
	before := s.pmClient.GetLastCacheUpdateTime()
	logger.InfoC(ctx, "Start to change tenant scheme, last update time for caches: %v", before)

	res := function()

	if !async {
		wait := 0
		for wait < timeout {
			logger.DebugC(ctx, "Wait for caches update, because of sync mode")
			if s.pmClient.GetLastCacheUpdateTime().After(before) {
				logger.InfoC(ctx, "Synchronization was completed successfully, waiting time: %d ms", wait)
				return res
			}
			time.Sleep(step * time.Millisecond)
			wait += step
		}
		return errors.New("Timeout for synchronization was exceeded")
	}
	return res
}

func (s *Synchronizer) Upsert(ctx context.Context, data domain.TenantDns) error {
	logger.DebugC(ctx, "Check if we can build hierarchy for tenant namespaces...")
	_, err := s.getCompositeNamespaceForTenant(ctx, data)
	if err != nil {
		return err
	}

	logger.DebugC(ctx, "Hierarchy was built successfully")

	err = s.generateNewUrlsForServicesIfNecessary(ctx, &data)
	if err != nil {
		return err
	}

	filteredData, err := s.filterGeneralRoutes(ctx, data)
	if err != nil {
		return err
	}

	site, exists := checkIfServiceExists(shoppingFrontendName, &filteredData)
	logger.InfoC(ctx, "Shopping exists = %v, site is %s", exists, site)
	if exists {
		if data.ServiceName == "" {
			logger.InfoC(ctx, "Service name is empty")
			scheme, err := s.dao.FindByTenantId(ctx, filteredData.TenantId)
			if err == nil && scheme.ServiceName != "" {
				filteredData.ServiceName = scheme.ServiceName
				logger.InfoC(ctx, "Service name from db %s", filteredData.ServiceName)
			} else {
				filteredData.ServiceName, err = s.generateUniqueServiceName(ctx, filteredData.TenantName)
				if err != nil {
					return err
				}
				logger.InfoC(ctx, "Generated service name")
			}
		}
		if err := changeServiceNameIfExists(shoppingFrontendName, filteredData.ServiceName, site, &filteredData); err != nil {
			return err
		}
	}
	if _, err := s.CheckCollisions(ctx, filteredData); err != nil {
		return err
	}

	if err := s.dao.Upsert(ctx, filteredData); err != nil {
		return err
	} else {
		s.Sync(ctx)
		return nil
	}
}

func (s *Synchronizer) generateNewUrlsForServicesIfNecessary(ctx context.Context, data *domain.TenantDns) error {
	_, ok := data.Sites[defaultNamespace]
	if !data.Active && ok {
		logger.InfoC(ctx, "Tenant is not in active state. Check if domain name or tenant name were changed")
		oldTenant, err := s.dao.FindByTenantId(ctx, data.TenantId)
		if err == nil {
			logger.DebugC(ctx, "For tenant id '%s' found scheme: %v", data.TenantId, oldTenant)
			if oldTenant.DomainName != data.DomainName || oldTenant.TenantName != data.TenantName {
				namespaces := data.Namespaces
				appendElemIfNotExists(&namespaces, s.pmClient.Namespace)

				if oldTenant.DomainName != data.DomainName {
					logger.InfoC(ctx, "Domain name was changed. Old value was '%s', new value is '%s'", oldTenant.DomainName, data.DomainName)
					publicServices, err := s.GetPublicServices(ctx, namespaces)
					if err != nil {
						return err
					}
					services, err := s.generateRoutesForServices(ctx, data.DomainName, publicServices, namespaces)
					if err != nil {
						return err
					}
					data.Sites[defaultNamespace] = services
				}

				if _, ok := data.Sites[defaultNamespace][shoppingFrontendName]; ok {
					logger.DebugC(ctx, "There is shopping frontend service in tenant scheme, generate new value for it")
					data.Sites[defaultNamespace][shoppingFrontendName] = s.generateShoppingRoute(ctx, data.DomainName, data.TenantName)
				}
			}
		}
	}
	return nil
}

func (s *Synchronizer) getCompositeNamespaceForTenant(ctx context.Context, data domain.TenantDns) (*domain.CompositeNamespace, error) {
	logger.InfoC(ctx, "Start getting composite namespace for tenant '%s'", data.TenantId)
	var childNameSpace *domain.CompositeNamespace
	namespaces := data.Namespaces
	for i, namespace := range namespaces {
		if namespace == s.pmClient.Namespace {
			if i < (len(namespaces) - 1) {
				namespaces = append(namespaces[:i], namespaces[i+1:]...)
			} else {
				namespaces = namespaces[:i]
			}
		}
	}

	if len(namespaces) == 1 {
		logger.DebugC(ctx, "There is only one namespace in list: %s, so we expect that it's a child for master namespace", namespaces[0])
		childNameSpace = &domain.CompositeNamespace{
			Namespace: namespaces[0],
			Child:     nil,
		}
	} else if len(namespaces) > 1 {
		configmaps, err := s.getConfigMapsForNamespaces(ctx, namespaces)
		if err != nil {
			return nil, err
		}
		childNameSpace, err = s.resolveChildForNamespace(ctx, s.pmClient.Namespace, configmaps)
		if err != nil {
			logger.ErrorC(ctx, "Error occurred while getting child namespace for '%s': %s", s.pmClient.Namespace, err.Error())
			return nil, err
		}
	}

	logger.InfoC(ctx, "Composite namespace was built successfully for tenant '%s'", data.TenantId)
	return &domain.CompositeNamespace{
		Namespace: s.pmClient.Namespace,
		Child:     childNameSpace,
	}, nil
}

func (s *Synchronizer) resolveChildForNamespace(ctx context.Context, parentNamespace string, configmaps map[string]mdomain.Configmap) (*domain.CompositeNamespace, error) {
	logger.DebugC(ctx, "Resolve child namespace for '%s'", parentNamespace)
	for namespace, configmap := range configmaps {
		parent := configmap.Data.Parent
		logger.DebugC(ctx, "For namespace '%s' configmap is %v, so parent is '%s'", namespace, configmap, parent)
		if parent == "" {
			return nil, errors.New(fmt.Sprintf("Parent was not found in configmap"))
		}
		if parent == parentNamespace {
			logger.DebugC(ctx, "Child for namespace '%s' was found: '%s'", parentNamespace, namespace)
			delete(configmaps, namespace)
			if len(configmaps) > 0 {
				logger.DebugC(ctx, "There are still some namespaces to resolve, try to go deeper")
				child, err := s.resolveChildForNamespace(ctx, namespace, configmaps)
				if err != nil {
					return nil, err
				}
				logger.DebugC(ctx, "Child for namespace '%s' was found correctly", parentNamespace)
				return &domain.CompositeNamespace{
					Namespace: namespace,
					Child:     child,
				}, nil
			} else {
				logger.DebugC(ctx, "No namespaces left, namespace '%s' has no children then", parentNamespace)
				return &domain.CompositeNamespace{
					Namespace: namespace,
					Child:     nil,
				}, nil
			}
		}
	}
	logger.ErrorC(ctx, "No child found for '%s'", parentNamespace)
	return nil, errors.New(fmt.Sprintf("Cannot build hierarchy for namespaces. No child for '%s' namespace", parentNamespace))
}

func (s *Synchronizer) getConfigMapsForNamespaces(ctx context.Context, namespaces []string) (map[string]mdomain.Configmap, error) {
	logger.DebugC(ctx, "Start getting configmaps for namespaces: %v", namespaces)
	configmaps := make(map[string]mdomain.Configmap)
	for _, namespace := range namespaces {
		configMap, err := s.pmClient.GetConfigMaps2(ctx, namespace, func(configmap *mdomain.Configmap) bool {
			if configmap.Metadata.Name == pmClient.ProjectTypeConfigMapName {
				return true
			} else {
				return false
			}
		})
		if err != nil {
			return nil, err
		} else if len(*configMap) == 0 {
			return nil, errors.New(fmt.Sprintf("Config map %s was not found for namespacae %s", pmClient.ProjectTypeConfigMapName, namespace))
		}
		configmaps[namespace] = (*configMap)[0]
	}
	logger.DebugC(ctx, "Return %d configmaps", len(configmaps))
	return configmaps, nil
}

func (s *Synchronizer) filterGeneralRoutes(ctx context.Context, data domain.TenantDns) (domain.TenantDns, error) {
	namespaces := data.Namespaces
	appendElemIfNotExists(&namespaces, s.pmClient.Namespace)
	generalRoutes := make([]mdomain.Route, 0)
	for _, namespace := range namespaces {
		namespaceGeneralRoutes, err := s.pmClient.GetRoutesByFilter(ctx, namespace, RouteIsGeneral)
		if err != nil {
			return data, err
		}
		generalRoutes = append(generalRoutes, *namespaceGeneralRoutes...)
	}

	for _, siteServices := range data.Sites {
		for service, addresses := range siteServices {
			for i, address := range addresses {
				if hostBelongsToRoutes(strings.ToLower(address.Host()), &generalRoutes) {
					switch len(addresses) {
					case 1:
						delete(siteServices, service)
					default:
						if i < (len(addresses) - 1) {
							addresses = append(addresses[:i], addresses[i+1:]...)
						} else {
							addresses = addresses[:i]
						}
					}
				}
			}
		}
	}

	return data, nil
}

func hostBelongsToRoutes(host string, routes *[]mdomain.Route) bool {
	for _, route := range *routes {
		if strings.ToLower(route.Spec.Host) == host {
			return true
		}
	}
	return false
}

func (s *Synchronizer) CheckCollisions(ctx context.Context, data domain.TenantDns) (domain.ValidationResult, error) {
	logger.DebugC(ctx, "Check received scheme: %v", data)
	scheme, err := s.dao.FindAll(ctx)
	if err != nil {
		return nil, err
	}

	validationResult := domain.NewValidationResult()
	s.schemeValidator.Check(data, *scheme, &validationResult)
	return validationResult, nil
}

func (s *Synchronizer) DeleteTenant(ctx context.Context, tenantId string) error {
	tenant, err := s.dao.FindByTenantId(ctx, tenantId)
	if err != nil {
		switch err.(type) {
		case exceptions.TenantNotFoundError:
			return nil
		default:
			return err
		}
	}
	tenant.Sites = make(map[string]domain.Services)
	tenant.Removed = true
	if err = s.dao.Upsert(ctx, *tenant); err != nil {
		return err
	} else {
		s.Sync(ctx)
		return nil
	}
}

func (s *Synchronizer) DeleteRoutes(ctx context.Context, tenantId string) error {
	tenant, err := s.dao.FindByTenantId(ctx, tenantId)
	if err != nil {
		switch err.(type) {
		case exceptions.TenantNotFoundError:
			return nil
		default:
			return err
		}
	}
	tenant.Sites = make(map[string]domain.Services)
	if err = s.dao.Upsert(ctx, *tenant); err != nil {
		return err
	} else {
		s.Sync(ctx)
		return nil
	}
}

func (s *Synchronizer) ChangeTenantStatus(ctx context.Context, tenantId string, active bool) error {
	if tenant, err := s.dao.FindByTenantId(ctx, tenantId); err != nil {
		logger.ErrorC(ctx, "Error occurred while searching for tenant: %s", tenantId)
		return err
	} else {
		tenant.Active = active
		logger.DebugC(ctx, "Change tenant %s status to %t", tenantId, active)
		return s.Upsert(ctx, *tenant)
	}
}

func (s *Synchronizer) buildSchemeIfRequired(ctx context.Context) error {
	logger.InfoC(ctx, "Check if it is necessary to build scheme from routes")
	if _, err := s.dao.FindInitInformation(ctx); err == nil {
		logger.InfoC(ctx, "Database was already initialized, no building required")
		return nil
	}

	scheme, err := s.getScheme(ctx)
	if err != nil {
		return err
	}

	logger.DebugC(ctx, "Scheme was built: %v", scheme)
	if len(*scheme) != 0 {
		for _, tenant := range *scheme {
			if tenant.TenantName == "" {
				tenantData, err := s.tenantClient.GetTenantByObjectId(ctx, tenant.TenantId)
				if err == nil {
					tenant.TenantName = tenantData.TenantName
					logger.InfoC(ctx, "Set tenantName during schema build %s for tenantId %s", tenant.TenantName, tenant.TenantId)
					if tenant.ServiceName == "" {
						tenant.ServiceName = tenantData.ServiceName
						logger.InfoC(ctx, "Set serviceName during schema build %s for tenantId %s", tenant.ServiceName, tenant.TenantId)
					}
				}
			}
			err = s.dao.Upsert(ctx, tenant)
			if err != nil {
				logger.ErrorC(ctx, "Error occurred while upserting tenant %s in db", tenant.TenantId)
				return err
			}
		}
	}

	logger.DebugC(ctx, "Scheme was built, set init information in db")
	err = s.dao.SetInitInformation(ctx, domain.Init{Initialized: true})
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while setting init information in db: %s", err.Error())
		return err
	}
	logger.InfoC(ctx, "Scheme was built successfully, init information was set in db")
	return nil
}

func (s *Synchronizer) getScheme(ctx context.Context) (*[]domain.TenantDns, error) {
	logger.DebugC(ctx, "It is necessary to build scheme from os routes")
	manageableRoutes, err := s.routesGetter(ctx, s.pmClient.Namespace, IsRouteManageable)
	if err != nil {
		logger.DebugC(ctx, "Error occurred while getting routes from os: %s", err.Error())
		return nil, err
	}
	return domain.FromRoutes(manageableRoutes), nil
}

func New(dao *pg.RouteManagerDao, osClient *pmClient.PaasMediationClient, mailSender *messaging.MailSender,
	autoRefreshTimeout time.Duration, platformHostname string, pgClient pgdbaas.PgClient,
	idpClient IDPFacade, isCompositeSatellite bool, baselineSM *composite.BaselineSM) *Synchronizer {
	ctx := context.Background()
	sync := &Synchronizer{
		dao,
		osClient,
		mailSender,
		autoRefreshTimeout,
		make(chan syncEvent, 16),
		validator.NewSchemeValidator(),
		osClient.GetRoutesByFilter,
		osClient.GetServices2,
		"",
		tm.NewClient(pgClient, websocket.NewConnector(), 10*time.Second),
		platformHostname,
		"https",
		idpClient,
		isCompositeSatellite,
		baselineSM,
	}
	sync.defaultDomainZone = configloader.GetKoanf().String("tenant.default.domain.zone")
	proto := configloader.GetKoanf().String("service.url.default.proto")
	logger.InfoC(ctx, "Read  SERVICE_URL_DEFAULT_PROTO %v", proto)
	if proto == "http" {
		logger.InfoC(ctx, "Set protocol to http")
		sync.protocol = proto
	}
	if err := sync.buildSchemeIfRequired(ctx); err != nil {
		panic(err)
	}

	go func() {
		if sync.compositeSatelliteMode {
			sync.tenantClient.SubscribeToAll(sync.SyncTenantsWithTM)
		}

		time.AfterFunc(5*time.Second, func() {
			sync.tenantClient.SubscribeToAll(sync.ActualizeActiveTenantsCache())
			sync.tenantClient.SubscribeToAllExcept(tm.EventTypeDeleted, sync.GetUpdateRedirectURIsInIDPHandler())
			sync.pmClient.AddRouteCallback(func(ctx context.Context, _ *pmClient.PaasMediationClient, _ pmClient.RouteUpdate) error {
				err := sync.SendRoutesToIDP(ctx)
				if err != nil {
					return errors.Wrap(err, 0)
				}
				return nil
			})
			sync.tenantClient.StartWatching(ctx)
		})
	}()

	osClient.StartSyncingCache(ctx)

	go func() {
		logger.InfoC(ctx, "Start processing synchronization")
		for {
			// by now event is only one type and it's used only for wait for notification to run sync
			<-sync.bus
			if err := sync.processSynchronization(ctx); err != nil {
				logger.ErrorC(ctx, err.Error())
			}
		}
	}()

	if sync.compositeSatelliteMode {
		sync.syncAllTenantsFromTM(ctx) // load all tenants from baseline TM
	}

	sync.StartAutoSyncTimer(ctx)

	return sync
}

func (s *Synchronizer) syncAllTenantsFromTM(ctx context.Context) {
	logger.InfoC(ctx, "sync all Tenants From TM")
	for {
		logger.DebugC(ctx, "Trying to get all tenants from tenant-manager...")
		tenants, err := s.tenantClient.GetAllTenantsByStatus(ctx, "")
		if err != nil {
			logger.ErrorC(ctx, "Failed attempt to get all tenants from tenant-manager: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		logger.DebugC(ctx, "upsert tenants from TM")
		tenantIdsFromTM := make(map[string]bool, len(*tenants))
		for _, tenant := range *tenants {
			tenantIdsFromTM[tenant.ObjectId] = true
			logger.DebugC(ctx, "upserting tenant %v", tenant)
			if err := s.upsertTenantFromTM(ctx, tenant); err != nil {
				logger.ErrorC(ctx, "Failed attempt to upsert tenant from tenant-manager: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}
		}

		logger.DebugC(ctx, "find all tenants")
		tenantDnsEntries, err := s.dao.FindAll(ctx)
		if err != nil {
			logger.ErrorC(ctx, "Failed attempt to get all tenant dns from DB: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		logger.DebugC(ctx, "deleting absent tenants")
		for _, tenantDns := range *tenantDnsEntries {
			if _, exists := tenantIdsFromTM[tenantDns.TenantId]; !exists {
				logger.DebugC(ctx, "deleting tenant %s", tenantDns.TenantId)
				if err := s.DeleteTenant(ctx, tenantDns.TenantId); err != nil {
					logger.ErrorC(ctx, "Failed attempt to delete tenant dns from DB: %v", err)
					time.Sleep(1 * time.Second)
					continue
				}
			}
		}
		break
	}
	logger.InfoC(ctx, "finish sync all Tenants From TM")
}

// this method is used for composite scenario
func (s *Synchronizer) upsertTenantFromTM(ctx context.Context, tenant tm.Tenant) error {
	namespaces := make([]string, 0)

	if tenant.Namespace != "" { // this is legacy logic for deprecated composite platform design
		namespaces = strings.Split(tenant.Namespace, ",")
	}
	isActive := tenant.Status == domain.Active

	tenantToUpdate := domain.TenantDns{
		TenantName:  tenant.TenantName,
		TenantId:    tenant.ObjectId,
		TenantAdmin: tenant.User.Email,
		Namespaces:  namespaces,
		DomainName:  tenant.DomainName,
		Active:      isActive,
		Sites:       domain.Sites{},
	}

	if isActive {
		tenantFromBase, err0 := s.FindByTenantId(ctx, tenant.ObjectId, "", false, true)
		if err0 != nil {
			logger.InfoC(ctx, "Searching of tenant with id %v finished with error %+v", tenant.ObjectId, err0)
			tenantToUpdate.Sites[defaultNamespace] = make(map[string]domain.AddressList, 0)
		} else {
			logger.DebugC(ctx, "Tenant from base %+v", tenantFromBase)
			tenantToUpdate.Sites = tenantFromBase.Sites
		}
	}

	if err := s.Upsert(ctx, tenantToUpdate); err != nil {
		logger.ErrorC(ctx, "Failed to upsert tenant with ObjectId %v: %v", tenant.ObjectId)
		return errors.New(err)
	}
	return nil
}

func (s *Synchronizer) SyncTenantsWithTM(ctx context.Context, tenantEvent tm.TenantWatchEvent) error {
	logger.InfoC(ctx, "Syncing tenants on event %v...", tenantEvent.Type)

	for _, tenantFromEvent := range tenantEvent.Tenants {
		logger.InfoC(ctx, "Handling tenant %+v", tenantFromEvent)

		if tenantEvent.Type == tm.EventTypeDeleted {
			if err := s.DeleteTenant(ctx, tenantFromEvent.ObjectId); err != nil {
				logger.ErrorC(ctx, "Failed to delete tenant with ObjectId %v: %v", tenantFromEvent.ObjectId)
				return errors.New(err)
			}
		} else {
			return s.upsertTenantFromTM(ctx, tenantFromEvent)
		}

	}
	return nil
}

func (s *Synchronizer) checkRedirectURISupport(ctx context.Context) bool {
	if ok, err := s.idpFacade.CheckPostURIFeature(ctx); err != nil {
		logger.PanicC(ctx, "Failed to check existence of PostURI feature on IPD: %s", err)
		return false
	} else {
		return ok
	}
}

func (s *Synchronizer) GetUpdateRedirectURIsInIDPHandler() func(context.Context, tm.TenantWatchEvent) error {
	return func(ctx context.Context, tenantEvent tm.TenantWatchEvent) error {
		err := s.SendRoutesToIDP(ctx)
		if err != nil {
			return errors.Wrap(err, 0)
		}
		return nil
	}
}

func (s *Synchronizer) ActualizeActiveTenantsCache() func(context.Context, tm.TenantWatchEvent) error {
	return func(ctx context.Context, tenantEvent tm.TenantWatchEvent) error {
		logger.DebugC(ctx, "Ready to update active tenants cache")
		if tenantEvent.Type == tm.EventTypeModified || tenantEvent.Type == tm.EventTypeSubscribed {
			s.tenantClient.UpdateActiveTenantsCache(ctx, tenantEvent.Tenants)
		}
		if tenantEvent.Type == tm.EventTypeDeleted {
			s.tenantClient.DeleteFromActiveTenantsCache(ctx, tenantEvent.Tenants)
		}
		return nil
	}
}

func (s *Synchronizer) GetPAASHostsWithTenantID(ctx context.Context, externalId string) ([]string, error) {
	routes, err := s.pmClient.GetRoutesByFilter(ctx, s.pmClient.Namespace, RouteHasTenantId(externalId))
	if err != nil {
		return nil, errors.Wrap(err, 0)
	}
	hosts := make([]string, len(*routes))
	for i, route := range *routes {
		hosts[i] = route.Spec.Host
	}
	return hosts, nil
}

func (s *Synchronizer) StartAutoSyncTimer(ctx context.Context) {
	logger.InfoC(ctx, "Start auto sync notifier")
	go func() {
		for range time.Tick(s.autoRefreshTimeout) {
			s.Sync(ctx)
			s.SendRoutesToIDP(ctx)
		}
	}()
}

func (s *Synchronizer) Sync(ctx context.Context) {
	logger.InfoC(ctx, "Generate event to force routes sync")

	select {
	case s.bus <- syncEvent{}:
		logger.DebugC(ctx, "Routes sync signal was successfully sent")
	default:
		logger.ErrorC(ctx, "Routes sync signal wasn't sent successfully")
	}
}

func (s *Synchronizer) processSynchronization(ctx context.Context) error {
	logger.InfoC(ctx, "Start routes sync procedure...")
	allSettings, err := s.dao.FindAll(ctx)
	if err != nil {
		logger.ErrorC(ctx, "Error get tenants from database: %s", err)
		return err
	}

	namespaces := s.getAllNamespacesFromTenants(allSettings)
	appendElemIfNotExists(&namespaces, s.pmClient.Namespace)
	manageableRoutes, err := s.pmClient.GetRoutesForNamespaces2(ctx, namespaces, IsRouteManageable)
	if err != nil {
		logger.ErrorC(ctx, "Error get manageable route list from openshift: %s "+fmt.Sprintf("%v", namespaces), err)
		return err
	}

	logger.DebugC(ctx, "Got manageable routes from openshift: %s", manageableRoutes)
	// delete manageable routes
	changedTenants := make(map[string]domain.TenantDns) // we need Set, but this shitty lang doesn't provide that
	logger.InfoC(ctx, "Start deleting routes from openshift which are not presented in database")
	for _, route := range *manageableRoutes {
		if !RouteIsGeneral(&route) && !hostBelongsToActiveTenant(ctx, route.Spec.Host, allSettings) {
			logger.DebugC(ctx, "Host %s is not present in database or belongs to non-active tenant and will be deleted", route.Spec.Host)
			err = s.pmClient.DeleteRoute(ctx, route.Metadata.Namespace, route.Metadata.Name)
			if err != nil {
				logger.ErrorC(ctx, "Error occurred while deleting route with name %s and host %s", route.Metadata.Name, route.Spec.Host)
			}
		}
	}
	logger.DebugC(ctx, "Delete tenants marked for removing")
	for _, tenant := range *allSettings {
		if tenant.Removed {
			logger.DebugC(ctx, "Remove tenant '%s' because removed flag is true", tenant.TenantId)
			s.dao.Delete(ctx, tenant.TenantId)
		}
	}
	logger.InfoC(ctx, "Deleting routes from openshift completed")

	logger.InfoC(ctx, "Start creating routes in openshift which are presented in database and absent in openshift")
	for _, tenantSites := range *allSettings {
		if tenantSites.Active {
			logger.DebugC(ctx, "Getting composite namespace for tenant %v", tenantSites)
			compositeNamespace, err := s.getCompositeNamespaceForTenant(ctx, tenantSites)
			if err == nil {
				for _, siteServices := range tenantSites.Sites {
					for service, addresses := range siteServices {
						for _, address := range addresses {
							logger.DebugC(ctx, "Resolving namespace for service %s", service)
							namespace, err := s.resolveNamespaceForService(ctx, service, *compositeNamespace)
							if err == nil {
								logger.DebugC(ctx, "Getting routes for service %s in namespace %s", service, namespace)
								currentNamespaceRoutes, err := s.pmClient.GetRoutes(ctx, namespace)
								if err == nil && !isHostPresentInOpenshiftRoutes(ctx, address, service, currentNamespaceRoutes) {
									logger.DebugC(ctx, "Host %s is not presented in openshift and will be created", address)
									s.pmClient.CreateRoute(ctx, tenantSites.ToRoute(service, address), namespace)
									changedTenants[tenantSites.TenantId] = tenantSites
								}
							}
						}
					}
				}
			}
		}
	}
	logger.InfoC(ctx, "Creating routes in openshift completed")

	if err != nil {
		logger.ErrorC(ctx, "Error occurred while getting common routes from openshift: %s", err)
	}
	for _, tenant := range changedTenants {
		namespaces := tenant.Namespaces
		appendElemIfNotExists(&namespaces, s.pmClient.Namespace)
		commonRoutes, err := s.pmClient.GetRoutesForNamespaces2(ctx, namespaces, RouteIsGeneral)
		if err != nil {
			logger.ErrorC(ctx, "Error occurred while getting common routes %s", err)
			logger.InfoC(ctx, "Routes sync finished with problem")
			return nil
		}
		messageContent := s.mailSender.GenerateTextForTenantUpdate(ctx, tenant, *commonRoutes)
		go s.mailSender.SendNotification(ctx, tenant.TenantAdmin, messageContent)
	}

	logger.InfoC(ctx, "Routes sync finished successfully")
	return nil
}

func (s *Synchronizer) resolveNamespaceForService(ctx context.Context, service string, compositeNamespace domain.CompositeNamespace) (string, error) {
	logger.InfoC(ctx, "Resolve namespace for service %s from namespaces %v", service, compositeNamespace)
	wait := 0
	namespace := ""
	var err error
	for wait < serviceTimeout {
		namespace, err = s.findServiceInNamespaces(ctx, service, compositeNamespace)
		logger.InfoC(ctx, "findServiceInNamespaces namespace: '%s'", namespace)
		if err == nil && namespace != "" {
			break
		}
		logger.InfoC(ctx, "Couldn't resolve namespace for service %s from namespaces %v. Try again...", service, compositeNamespace)
		s.pmClient.InitServicesMapInCache(ctx, namespace)
		time.Sleep(step * time.Millisecond)
		wait += step
	}
	if err != nil {
		return "", err
	} else if namespace != "" {
		return namespace, nil
	}

	return "", errors.New(fmt.Sprintf("Service %s wasn't found in any namespace: %v or master namespace %s", service, compositeNamespace, s.pmClient.Namespace))
}

func (s *Synchronizer) findServiceInNamespaces(ctx context.Context, service string, compositeNamespace domain.CompositeNamespace) (string, error) {
	logger.DebugC(ctx, "Try to find service '%s' in namespace '%s'", service, compositeNamespace.Namespace)
	if compositeNamespace.Child != nil {
		namespace, err := s.findServiceInNamespaces(ctx, service, *compositeNamespace.Child)
		if err != nil {
			return "", err
		} else if namespace != "" {
			return namespace, nil
		}
	}
	services, err := s.servicesGetter(ctx, compositeNamespace.Namespace, func(openshiftService *mdomain.Service) bool {
		return openshiftService.Metadata.Name == service
	})
	if err != nil {
		return "", err
	} else if len(*services) > 0 {
		logger.InfoC(ctx, "Namespace for service %s was resolved. Result: %s", service, compositeNamespace.Namespace)
		return compositeNamespace.Namespace, nil
	}

	logger.InfoC(ctx, "Service '%s' wasn't found in namespace '%s' or in children", service, compositeNamespace.Namespace)
	return "", nil
}

func isHostPresentInOpenshiftRoutes(ctx context.Context, host domain.Address, service string, routes *[]mdomain.Route) bool {
	logger.DebugC(ctx, "Search for host %s in openshift routes", host)
	for _, route := range *routes {
		if route.Spec.Host == host.Host() {
			if service == route.Spec.Service.Name {
				logger.DebugC(ctx, "Host %s was found in database routes", host)
			} else {
				logger.ErrorC(ctx, "Host %s was found in database routes for service: %s but expected for: %s", host, route.Spec.Service.Name, service)
			}
			return true
		}
	}
	logger.DebugC(ctx, "Host %s was not found in openshift routes", host)
	return false
}

func hostBelongsToActiveTenant(_ context.Context, host string, allSettings *[]domain.TenantDns) bool {
	lowerValue := strings.ToLower(host)
	for _, tenantSites := range *allSettings {
		if tenantSites.Active {
			for _, siteServices := range tenantSites.Sites {
				for _, addresses := range siteServices {
					for _, address := range addresses {
						if strings.ToLower(address.Host()) == lowerValue {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

func IsVirtual(r mdomain.Metadata) bool {
	value, ok := serviceloader.MustLoad[utils.AnnotationMapper]().Get(r.Annotations, "tenant.service.type")
	return ok && value == "virtual"
}

func RouteIsGeneral(r *mdomain.Route) bool {
	value, ok := serviceloader.MustLoad[utils.AnnotationMapper]().Get(r.Metadata.Annotations, "tenant.service.tenant.id")
	return ok && value == "GENERAL"
}

func RouteHasTenantId(tenantId string) func(r *mdomain.Route) bool {
	return func(r *mdomain.Route) bool {
		value, ok := serviceloader.MustLoad[utils.AnnotationMapper]().Get(r.Metadata.Annotations, "tenant.service.tenant.id")
		return ok && value == tenantId
	}
}

func IsRouteManageable(r *mdomain.Route) bool {
	value, ok := serviceloader.MustLoad[utils.AnnotationMapper]().Get(r.Metadata.Annotations, "tenant.service.tenant.id")
	return ok && strings.Compare(value, "GENERAL") != 0
}

func (s *Synchronizer) buildCustomServicesFromRoutes(ctx context.Context, routes *[]mdomain.Route, protocol string, namespaces []string) (*[]mdomain.CustomService, error) {
	logger.DebugC(ctx, "Build custom serices from routes: %v", routes)
	filteredRoutes := filterRoutes(routes)
	var services []mdomain.CustomService

	for _, route := range *filteredRoutes {
		logger.DebugC(ctx, "Starting processing route: %v", route)
		service, err := s.buildCustomServiceFromRoute(ctx, route, protocol, namespaces)
		if err != nil {
			return nil, err
		}
		services = append(services, *service)
	}

	logger.DebugC(ctx, "Services were built: %v", services)
	for index, service := range services {
		if service.Id == shoppingFrontendName {
			services[index].Name = "Shopping Catalogue"
			services[index].Description = "Market for your customers"
		}
	}

	return &services, nil
}

func (s *Synchronizer) buildCustomServiceFromRoute(ctx context.Context, route mdomain.Route, protocol string, namespaces []string) (*mdomain.CustomService, error) {
	id, err := s.resolveField(
		ctx,
		route.Spec.Service.Name,
		func() string { return route.GetServiceId("") },
		func(s mdomain.Service) string { return s.GetId() },
		route.Spec.Service.Name,
		namespaces)
	if err != nil {
		return nil, err
	}

	serviceName, err := s.resolveField(
		ctx,
		route.Spec.Service.Name,
		func() string { return route.GetServiceName() },
		func(s mdomain.Service) string { return s.GetShowName() },
		route.Metadata.Name,
		namespaces)
	if err != nil {
		return nil, err
	}
	url, err := s.resolveCustomServiceUrl(ctx, route, protocol, namespaces)
	if err != nil {
		return nil, err
	}

	description, err := s.resolveField(
		ctx,
		route.Spec.Service.Name,
		func() string { return route.GetServiceDescription() },
		func(s mdomain.Service) string { return s.GetDescription() },
		"",
		namespaces)
	if err != nil {
		return nil, err
	}

	service := mdomain.CustomService{
		Id:          id,
		Name:        serviceName,
		URL:         url,
		Description: description,
	}

	return &service, nil
}

func (s *Synchronizer) resolveField(ctx context.Context, serviceName string, fromRoute func() string, fromService func(mdomain.Service) string, defaultValue string, namespaces []string) (string, error) {
	logger.DebugC(ctx, "Resolve field for service %s", serviceName)
	if field := fromRoute(); field != "" {
		logger.DebugC(ctx, "Got value from route: %s", field)
		return field, nil
	}

	services, err := s.pmClient.GetServicesForNamespaces2(ctx, namespaces, func(s *mdomain.Service) bool { return s.Metadata.Name == serviceName })
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while getting service %s", serviceName)
		return "", err
	} else if services == nil {
		return "", nil
	} else if len(*services) == 0 {
		logger.ErrorC(ctx, "No service found with name %s", serviceName)
		return defaultValue, nil
	} else if field := fromService((*services)[0]); field != "" {
		logger.DebugC(ctx, "Got value from service: %s", field)
		return field, nil
	}
	logger.DebugC(ctx, "Use default value: %s", defaultValue)
	return defaultValue, nil
}

func (s *Synchronizer) resolveCustomServiceUrl(ctx context.Context, route mdomain.Route, protocol string, namespaces []string) (string, error) {
	logger.DebugC(ctx, "Resolve custom service url for route: %v", route)
	if protocol == "" {
		protocol = s.protocol
	}
	suffix, err := s.resolveField(
		ctx,
		route.Spec.Service.Name,
		func() string { return route.GetServiceSuffix() },
		func(s mdomain.Service) string { return s.GetSuffix() },
		"",
		namespaces)
	if err != nil {
		return "", err
	}
	path := strings.TrimPrefix(strings.Replace(fmt.Sprintf("%s/%s", route.Spec.Path, suffix), "//", "/", -1), "/")
	logger.DebugC(ctx, "Generate url for service %s with protocol %s host %s and path %s", route.Metadata.Name, protocol, route.Spec.Host, path)
	return fmt.Sprintf("%s://%s/%s", protocol, route.Spec.Host, path), nil
}

func (s *Synchronizer) generateUniqueServiceName(ctx context.Context, tenantName string) (string, error) {
	logger.InfoC(ctx, "tenant name in generate service name %s", tenantName)
	optionalPart := tenantName
	serviceName := tenantPrefix + optionalPart
	logger.InfoC(ctx, "sevice name in generate service name %s", serviceName)

	if len(serviceName) > nameMaxLength {
		serviceName = serviceName[0:nameMaxLength]
	}

	exists, err := s.serviceNameAlreadyExists(ctx, serviceName)
	if err != nil {
		return "", err
	}

	for exists {
		logger.InfoC(ctx, "service name exist")
		counter := 1
		if len(serviceName+strconv.FormatInt(int64(counter), 10)) <= nameMaxLength {
			serviceName = serviceName + strconv.Itoa(counter)
			counter++
		} else {
			if len(tenantName) > nameMaxLength/2 {
				optionalPart = tenantName[0 : nameMaxLength/2]
			}
			serviceName = optionalPart + strconv.Itoa(rand.Int())
			if len(serviceName) > nameMaxLength {
				serviceName = serviceName[0:nameMaxLength]
			}
		}
		exists, err = s.serviceNameAlreadyExists(ctx, serviceName)
		if err != nil {
			return "", err
		}
	}
	logger.InfoC(ctx, "final sevice name in generate service name %s", serviceName)
	return serviceName, nil
}

func (s *Synchronizer) serviceNameAlreadyExists(ctx context.Context, serviceName string) (bool, error) {
	services, err := s.pmClient.GetServices2(ctx, s.pmClient.Namespace, func(service *mdomain.Service) bool {
		return service.Metadata.Name == serviceName
	})
	if err != nil {
		return false, err
	}
	return len(*services) > 0, nil
}

func (s *Synchronizer) CreateVirtualService(ctx context.Context, serviceReq domain.ServiceRegistration) error {
	namespace := s.pmClient.Namespace
	route := serviceReq.ToRoute(s.platformHostname, namespace)
	if msg, valid := serviceReq.IsRouteValid(route, namespace); !valid {
		return wrappers.ErrorWrapper{StatusCode: http.StatusBadRequest, Message: msg}
	}

	err := s.pmClient.CreateService(ctx, serviceReq.ToService(), namespace)
	if err != nil {
		logger.ErrorC(ctx, "Error while creating service %v", err)
		return err
	}

	err = s.pmClient.CreateRoute(ctx, route, namespace)
	if err != nil {
		logger.ErrorC(ctx, "Error while creating route %v", err)
		return err
	}
	err = s.forceUpdateIDPRouteCache(ctx)
	if err != nil {
		logger.ErrorC(ctx, "Error while updating route cache on IDP %v", err)
		return err
	}
	return s.dao.AddRouteToTenants(ctx, route.Spec.Host, serviceReq.VirtualService)
}

func (s *Synchronizer) UpdateOrCreateVirtualService(ctx context.Context, serviceReq domain.ServiceRegistration) error {
	namespace := s.pmClient.Namespace
	serviceName := serviceReq.VirtualService

	route := serviceReq.ToRoute(s.platformHostname, namespace)
	if msg, valid := serviceReq.IsRouteValid(route, namespace); !valid {
		return wrappers.ErrorWrapper{StatusCode: http.StatusBadRequest, Message: msg}
	}

	services, err := s.pmClient.GetServices2(ctx, namespace, func(service *mdomain.Service) bool {
		return !IsVirtual(service.Metadata) && service.Metadata.Name == serviceName
	})

	if err != nil {
		logger.ErrorC(ctx, "Error while getting virtual services to update %v", err)
		return err
	}

	if len(*services) > 0 {
		logger.ErrorC(ctx, "Next services were found by name %s: %v", serviceName, services)
		return wrappers.ErrorWrapper{StatusCode: http.StatusForbidden, Message: "Service " + serviceName + " found, but service is not virtual"}
	}

	err = s.pmClient.UpdateOrCreateService(ctx, serviceReq.ToService(), namespace)
	if err != nil {
		logger.ErrorC(ctx, "Error while updating service %v", err)
		return err
	}

	routesToUpdate, err := s.pmClient.GetRoutesByFilter(ctx, s.pmClient.Namespace, func(route *mdomain.Route) bool {
		return route.Spec.Service.Name == serviceName
	})

	if err != nil {
		logger.ErrorC(ctx, "Error while getting routes to delete %v", err)
		return err
	}

	if len(*routesToUpdate) == 0 {
		err = s.pmClient.UpdateOrCreateRoute(ctx, route, namespace)
		if err != nil {
			logger.ErrorC(ctx, "Error while updating route %v", err)
			return err
		}
		err := s.forceUpdateIDPRouteCache(ctx)
		if err != nil {
			logger.ErrorC(ctx, "Error while updating route cache on IDP %v", err)
			return err
		}
		return s.dao.AddRouteToTenants(ctx, route.Spec.Host, serviceReq.VirtualService)
	} else {
		if route.Spec.Host != (*routesToUpdate)[0].Spec.Host {
			return wrappers.ErrorWrapper{StatusCode: http.StatusForbidden, Message: "Can't update host field. Host field is immutable"}
		}
		for _, routeToUpdate := range *routesToUpdate {
			routeToUpdate.MergeRoute(route)
			logger.DebugC(ctx, "Try to update route %+v", routeToUpdate)
			err = s.pmClient.UpdateOrCreateRoute(ctx, &routeToUpdate, namespace)
			if err != nil {
				logger.ErrorC(ctx, "Error while updating route %v", err)
				return err
			}
		}
	}

	return nil
}

func (s *Synchronizer) DeleteVirtualService(ctx context.Context, serviceName string) error {
	namespace := s.pmClient.Namespace

	services, err := s.pmClient.GetServices2(ctx, namespace, func(service *mdomain.Service) bool {
		return IsVirtual(service.Metadata) && service.Metadata.Name == serviceName
	})

	if err != nil {
		logger.ErrorC(ctx, "Error while getting virtual services to delete %v", err)
		return err
	}

	if len(*services) == 0 {
		return wrappers.ErrorWrapper{StatusCode: http.StatusNotFound, Message: "Virtual service " + serviceName + " not found"}
	}

	err = s.pmClient.DeleteService(ctx, serviceName, namespace)
	if err != nil {
		logger.ErrorC(ctx, "Error while creating service %v", err)
		return err
	}

	routesToDelete, err := s.pmClient.GetRoutesByFilter(ctx, s.pmClient.Namespace, func(route *mdomain.Route) bool {
		return route.Spec.Service.Name == serviceName
	})

	if err != nil {
		logger.ErrorC(ctx, "Error while getting routes to delete %v", err)
		return err
	}
	for _, routeToDelete := range *routesToDelete {
		logger.DebugC(ctx, "Try to delete route %+v", routeToDelete)
		err = s.pmClient.DeleteRoute(ctx, namespace, routeToDelete.Metadata.Name)
		if err != nil {
			logger.ErrorC(ctx, "Error while deleting route %v", err)
			return err
		}
	}

	return s.dao.DeleteRouteFromTenants(ctx, serviceName)
}

func (s Synchronizer) forceUpdateIDPRouteCache(ctx context.Context) error {
	logger.DebugC(ctx, "Sending request to update idp route cache")
	idpServerAddr := configloader.GetKoanf().MustString("identity.provider.url")
	response, err := utils.DoRetryRequest(ctx, fasthttp.MethodPost, idpServerAddr+"/auth/actions/frontend", nil, logger)
	if err == nil {
		defer fasthttp.ReleaseResponse(response)
	}

	if err != nil && response.StatusCode() != fasthttp.StatusNoContent {
		logger.ErrorC(ctx, "Can't refresh route cache in IDP: %v", err)
		return wrappers.ErrorWrapper{StatusCode: response.StatusCode(),
			Message: fmt.Sprintf("Can't update route cache in IDP. Got status code: err: %v", err)}
	}
	return nil
}

func (s *Synchronizer) RegisterTenant(ctx context.Context, tenant tm.Tenant) error {
	logger.DebugC(ctx, "Register tenant in site-management: %v", tenant)
	namespaces := make([]string, 0)

	if tenant.Namespace != "" {
		namespaces = strings.Split(tenant.Namespace, ",")
	}
	tenantDns := &domain.TenantDns{
		TenantId:   tenant.ObjectId,
		Namespaces: namespaces,
		TenantName: tenant.TenantName,
	}
	return s.dao.SaveTenant(ctx, tenantDns)
}

func filterRoutes(routes *[]mdomain.Route) *[]mdomain.Route {
	sortedRoutesMap := buildSortedMapFromRoutes(routes)
	logger.Debug("Sorted map from routes: %v", sortedRoutesMap)

	var filteredRoutes []mdomain.Route
	for _, sortedRoutes := range sortedRoutesMap {
		filteredRoutes = append(filteredRoutes, sortedRoutes[0])
	}
	return &filteredRoutes
}

func buildSortedMapFromRoutes(routes *[]mdomain.Route) map[string][]mdomain.Route {
	sortedRoutesMap := make(map[string][]mdomain.Route)

	for _, route := range *routes {
		serviceId := route.GetServiceId(route.Spec.Service.Name)
		sortedRoutesMap[serviceId] = append(sortedRoutesMap[serviceId], route)
	}

	for _, routes := range sortedRoutesMap {
		sort.Slice(routes, func(i, j int) bool { return routes[i].GetPriority() > routes[j].GetPriority() })
	}
	return sortedRoutesMap
}

func changeServiceNameIfExists(from, to, site string, tenant *domain.TenantDns) error {
	for index, runeValue := range to {
		if !unicode.IsPrint(runeValue) {
			logger.Info("Service Name %s is incorrect. Unprintable %#U at position %d.", to, runeValue, index)
			return fmt.Errorf("Passed tenant serviceName %s is incorrect. It contains a forbidden symbol %#U at byte position %d.", to, runeValue, index)
		}
	}

	if service, ok := tenant.Sites[site][from]; ok {
		logger.Info("Delete service %s from site %s and set to %s", from, site, to)
		delete(tenant.Sites[site], from)
		tenant.Sites[site][to] = service
	}
	return nil
}

func checkIfServiceExists(serviceName string, tenant *domain.TenantDns) (string, bool) {
	for site, services := range tenant.Sites {
		if _, ok := services[serviceName]; ok {
			return site, true
		}
	}
	return "", false
}

func appendElemIfNotExists(elems *[]string, elem string) {
	for _, currentElem := range *elems {
		if currentElem == elem {
			return
		}
	}
	*elems = append(*elems, elem)
}

func getTenantRoutesFromOpenshiftRoutes(ctx context.Context, routes *[]mdomain.Route) ([]string, map[string][]string) {
	logger.InfoC(ctx, "Get realms from openshift routes")
	commonRoutes := []string{}

	tenantRoutes := make(map[string][]string)
	for _, route := range *routes {
		tenantObjectId := route.GetTenantId()
		host := route.Spec.Host
		if host == "" {
			logger.WarnC(ctx, "Route with name %s has empty host.", route.Metadata.Name)
		}
		if tenantObjectId == "" || tenantObjectId == "GENERAL" {
			commonRoutes = append(commonRoutes, host)
		} else { // if tenantId was specified in route annotation
			if tenantHosts, ok := tenantRoutes[tenantObjectId]; ok {
				tenantRoutes[tenantObjectId] = append(tenantHosts, host)
			} else {
				tenantHosts := []string{host}
				tenantRoutes[tenantObjectId] = tenantHosts
			}
		}
	}

	logger.InfoC(ctx, "Return %d common routes and %d realms", len(commonRoutes), len(tenantRoutes))
	return commonRoutes, tenantRoutes
}
