package synchronizer

import (
	"context"
	"fmt"
	"github.com/netcracker/qubership-core-lib-go/v3/serviceloader"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/domain"
	pmClient "github.com/netcracker/qubership-core-site-management/site-management-service/v2/paasMediationClient"
	mdomain "github.com/netcracker/qubership-core-site-management/site-management-service/v2/paasMediationClient/domain"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/tm"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/utils"
	"github.com/stretchr/testify/assert"
	"reflect"
	"testing"
	"time"
)

const (
	oldServiceName  = "shopping-frontend"
	newServiceName  = "qwerty"
	defaultSiteName = "default"
)

var (
	ctx        = context.Background()
	firstRoute = mdomain.Route{
		Spec: mdomain.RouteSpec{Host: "test.first_test_service",
			Path:    "",
			Service: mdomain.Target{Name: "first_test_service"},
		},
		Metadata: mdomain.Metadata{Name: "first_test_route",
			Namespace: "test_namespace",
			Annotations: map[string]string{
				"qubership.cloud/tenant.service.id":        "first_test_service_id",
				"qubership.cloud/tenant.service.tenant.id": "GENERAL"}},
	}

	secondRoute = mdomain.Route{
		Spec: mdomain.RouteSpec{Host: "test.second_test_service",
			Path:    "",
			Service: mdomain.Target{Name: "second_test_service"},
		},
		Metadata: mdomain.Metadata{Name: "second_test_route",
			Namespace: "test_namespace",
			Annotations: map[string]string{
				"qubership.cloud/tenant.service.id":        "second_test_service_id",
				"qubership.cloud/tenant.service.tenant.id": "test_tenant_id"}},
	}

	thirdRoute = mdomain.Route{
		Spec: mdomain.RouteSpec{Host: "test.first_test_service_site",
			Path:    "",
			Service: mdomain.Target{Name: "first_test_service"},
		},
		Metadata: mdomain.Metadata{Name: "third_test_route",
			Namespace: "test_namespace",
			Annotations: map[string]string{
				"qubership.cloud/tenant.service.id":        "first_test_service_id",
				"qubership.cloud/tenant.service.tenant.id": "test_tenant_id"}},
	}

	generalRoute = mdomain.Route{
		Spec: mdomain.RouteSpec{Host: "general.test.first_test_service_site",
			Path:    "",
			Service: mdomain.Target{Name: "first_test_service"},
		},
		Metadata: mdomain.Metadata{Name: "general_test_route",
			Namespace: "test_namespace",
			Annotations: map[string]string{
				"qubership.cloud/tenant.service.id":        "first_test_service_id",
				"qubership.cloud/tenant.service.tenant.id": "GENERAL"}},
	}

	customRoute = mdomain.Route{
		Spec: mdomain.RouteSpec{Host: "general.test.first_test_service_site",
			Path:    "",
			Service: mdomain.Target{Name: "first_test_service"},
		},
		Metadata: mdomain.Metadata{Name: "custom_test_route",
			Namespace: "test_namespace",
			Annotations: map[string]string{
				"qubership.cloud/tenant.service.id": "first_test_service_id"}},
	}

	routes = []mdomain.Route{firstRoute, secondRoute, thirdRoute, generalRoute, customRoute}

	configMapFirstNamespace = mdomain.Configmap{
		Data: mdomain.ConfigMapData{
			Parent: "parentNamespace",
		},
		Metadata: mdomain.ConfigMapMetaData{
			Namespace: "firstNamespace",
			Name:      pmClient.ProjectTypeConfigMapName,
		},
	}
	configMapSecondNamespace = mdomain.Configmap{
		Data: mdomain.ConfigMapData{
			Parent: "firstNamespace",
		},
		Metadata: mdomain.ConfigMapMetaData{
			Namespace: "secondNamespace",
			Name:      pmClient.ProjectTypeConfigMapName,
		},
	}
	configMapThirdNamespace = mdomain.Configmap{
		Data: mdomain.ConfigMapData{
			Parent: "secondNamespace",
		},
		Metadata: mdomain.ConfigMapMetaData{
			Namespace: "thirdNamespace",
			Name:      pmClient.ProjectTypeConfigMapName,
		},
	}
	configMaps = make(map[string]mdomain.Configmap)

	compositeNamespace = domain.CompositeNamespace{
		Namespace: "firstNamespace",
		Child: &domain.CompositeNamespace{
			Namespace: "secondNamespace",
			Child: &domain.CompositeNamespace{
				Namespace: "thirdNamespace",
				Child:     nil,
			},
		},
	}

	tenant = domain.TenantDns{
		Sites: domain.Sites{
			defaultSiteName: domain.Services{
				oldServiceName: domain.AddressList{
					"tenanttestqa2.backup.openshift.sdntest.qubership.org/welcome",
					"www.tenanttestqa2.backup.openshift.sdntest.qubership.org/welcome",
				},
			},
		},
	}

	serviceFromParentNamespace = mdomain.Service{
		Metadata: mdomain.Metadata{
			Namespace: "firstNamespace",
			Name:      "test-service",
			Annotations: map[string]string{
				"qubership.cloud/tenant.service.tenant.id": "GENERAL",
			},
		},
	}

	serviceFromChildNamespace = mdomain.Service{
		Metadata: mdomain.Metadata{
			Namespace: "thirdNamespace",
			Name:      "test-service",
			Annotations: map[string]string{
				"qubership.cloud/tenant.service.tenant.id": "GENERAL",
			},
		},
	}
)

func init() {
	serviceloader.Register(1, utils.NewBaseAnnotationGetter("qubership.cloud"))
}

func TestFindServicesInNamespace(t *testing.T) {
	sync := Synchronizer{
		servicesGetter: GetServices2,
	}

	namespace, err := sync.findServiceInNamespaces(ctx, "test-service", compositeNamespace)

	if err != nil || namespace != "thirdNamespace" {
		t.Fatal()
	}
}

func TestServiceIsNotPresentInNamespaces(t *testing.T) {
	sync := Synchronizer{
		servicesGetter: GetServices2,
	}

	namespace, err := sync.findServiceInNamespaces(ctx, "notExistingService", compositeNamespace)

	if err != nil || namespace != "" {
		t.Fatal()
	}
}

func TestReplaceService(t *testing.T) {
	changeServiceNameIfExists(oldServiceName, newServiceName, defaultSiteName, &tenant)
	if _, ok := tenant.Sites[defaultSiteName][newServiceName]; !ok {
		t.Fatal()
	}

	if err := changeServiceNameIfExists(newServiceName, newServiceName+"", defaultSiteName, &tenant); err == nil {
		t.Fatal()
	}
}

func TestResolveNamespacesHierarchy(t *testing.T) {
	configMaps["firstNamespace"] = configMapFirstNamespace
	configMaps["secondNamespace"] = configMapSecondNamespace
	configMaps["thirdNamespace"] = configMapThirdNamespace
	sync := Synchronizer{
		pmClient: &pmClient.PaasMediationClient{
			Namespace: "namespace",
		},
		routesGetter: GetRoutes2,
	}

	result, err := sync.resolveChildForNamespace(ctx, "parentNamespace", configMaps)

	if err != nil {
		t.Fatal(err.Error())
	}

	if result.Namespace != "firstNamespace" ||
		result.Child == nil ||
		result.Child.Namespace != "secondNamespace" ||
		result.Child.Child == nil ||
		result.Child.Child.Namespace != "thirdNamespace" ||
		result.Child.Child.Child != nil {
		t.Fatal()
	}
}

func TestIsHostPresentInOpenshiftRoutesTrue(t *testing.T) {
	host := "test.first_test_service_site"
	service := "first_test_service"

	result := isHostPresentInOpenshiftRoutes(ctx, domain.Address(host), service, &routes)

	if result != true {
		t.Fatal()
	}
}

func TestIsHostPresentInOpenshiftRoutesFalse(t *testing.T) {
	host := "test.first_test_service_site_not_exists"
	service := "first_test_service"

	result := isHostPresentInOpenshiftRoutes(ctx, domain.Address(host), service, &routes)

	if result != false {
		t.Fatal()
	}
}

func TestFilterRoutes(t *testing.T) {
	filteredRoutes := filterRoutes(&routes)

	if len(*filteredRoutes) != 2 {
		t.Fatal()
	}

	fmt.Println(*filteredRoutes)
	for _, route := range *filteredRoutes {
		if reflect.DeepEqual(route, firstRoute) {
			t.Fatal()
		}
	}
}

func TestMigration(t *testing.T) {
	sync := Synchronizer{
		pmClient: &pmClient.PaasMediationClient{
			Namespace: "namespace",
		},
		routesGetter: GetRoutes2,
	}

	scheme, err := sync.getScheme(nil)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(*scheme)
	for _, tenant := range *scheme {
		if !tenant.Active {
			t.Fatal("tenant is not active")
		}
		for _, siteServices := range tenant.Sites {
			for _, addresses := range siteServices {
				for _, address := range addresses {
					if address.Host() == firstRoute.Spec.Host || address.Host() == generalRoute.Spec.Host {
						t.Fatal("general host is present in scheme: " + address.Host())
					} else if address.Host() == customRoute.Spec.Host {
						t.Fatal("custom host is present in scheme: " + address.Host())
					}
				}
			}
		}
	}
}

func TestGetIdentityProviderRouteDoNotReturnIdpIngressButReturnsPublicGW_Baseline(t *testing.T) {
	sync := Synchronizer{compositeSatelliteMode: false}
	publicGwUrl := "https://public-gateway-service"
	services := []mdomain.CustomService{
		{
			Id:          domain.IdentityProviderId,
			Name:        "Identity-Provider",
			URL:         "https://identity-provider-ingress/",
			Description: "Idp ingress",
		},
		{
			Id:          domain.PublicGatewayServiceId,
			Name:        "Public-Gateway",
			URL:         publicGwUrl,
			Description: "Public gateway",
		},
	}
	gotServices, err := sync.getIdentityProviderRoute(context.Background(), nil, services)
	assert.Nil(t, err)
	assert.Len(t, gotServices, 1)
	assert.Equal(t, domain.IdentityProviderId, gotServices[0].Id)
	assert.Equal(t, publicGwUrl, gotServices[0].URL)
}

func TestSynchronizer_ActivateTenants_ModifiedWatchEvent(t *testing.T) {
	testBehaviorWithEvent(tm.EventTypeModified, t)
}

func TestSynchronizer_ActivateTenants_SubscribedWatchEvent(t *testing.T) {
	testBehaviorWithEvent(tm.EventTypeSubscribed, t)
}

func TestSynchronizer_ActivateTenants_DeletedWatchEvent(t *testing.T) {
	ctx := context.Background()
	tenantClient := tm.NewClient(nil, nil, time.Second)
	sync := Synchronizer{
		tenantClient: tenantClient,
	}
	assert.Empty(t, sync.tenantClient.GetActiveTenantsCache(ctx))
	tenants := createTestTenants()
	event := tm.TenantWatchEvent{Type: tm.EventTypeSubscribed, Tenants: tenants}
	err := sync.ActualizeActiveTenantsCache()(ctx, event)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(sync.tenantClient.GetActiveTenantsCache(ctx)))

	tenantForDeletion := tenants[0]
	deleteEvent := tm.TenantWatchEvent{Type: tm.EventTypeDeleted, Tenants: []tm.Tenant{tenantForDeletion}}
	deletionErr := sync.ActualizeActiveTenantsCache()(ctx, deleteEvent)
	assert.Nil(t, deletionErr)
	assert.Equal(t, 1, len(sync.tenantClient.GetActiveTenantsCache(ctx)))
}

func testBehaviorWithEvent(eventType tm.TenantWatchEventType, t *testing.T) {
	ctx := context.Background()
	tenantClient := tm.NewClient(nil, nil, time.Second)
	sync := Synchronizer{
		tenantClient: tenantClient,
	}
	assert.Empty(t, sync.tenantClient.GetActiveTenantsCache(ctx))
	tenants := createTestTenants()
	event := tm.TenantWatchEvent{Type: eventType, Tenants: tenants}
	err := sync.ActualizeActiveTenantsCache()(ctx, event)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(sync.tenantClient.GetActiveTenantsCache(ctx)))
}

func createTestTenants() []tm.Tenant {
	tenants := []tm.Tenant{
		{ExternalId: "1", Status: tm.StatusActive},
		{ExternalId: "2", Status: tm.StatusActive},
		{ExternalId: "3", Status: "not-active"},
	}
	return tenants
}

func GetRoutes2(ctx context.Context, namespace string, filter func(*mdomain.Route) bool) (*[]mdomain.Route, error) {
	result := make([]mdomain.Route, 0)
	for _, route := range routes {
		if filter(&route) {
			result = append(result, route)
		}
	}
	return &result, nil
}

func GetServices2(ctx context.Context, namespace string, filter func(service *mdomain.Service) bool) (*[]mdomain.Service, error) {
	switch namespace {
	case "firstNamespace":
		if filter(&serviceFromParentNamespace) {
			return &[]mdomain.Service{serviceFromParentNamespace}, nil
		} else {
			return &[]mdomain.Service{}, nil
		}
	case "thirdNamespace":
		if filter(&serviceFromChildNamespace) {
			return &[]mdomain.Service{serviceFromChildNamespace}, nil
		} else {
			return &[]mdomain.Service{}, nil
		}
	default:
		return &[]mdomain.Service{}, nil
	}
}

func TestSynchronizer_extractSiteByHost_HostInBothSites(t *testing.T) {
	sites := domain.Sites{
		"brand-3": domain.Services{
			"service-1": domain.AddressList{
				"service-1.default",
			},
			"service-2": domain.AddressList{
				"service-2.brand-3",
			},
		},
		"default": domain.Services{
			"service-1": domain.AddressList{
				"service-1.default",
			},
			"service-2": domain.AddressList{
				"service-2.default",
			},
		},
	}

	assert.Equal(t, "brand-3", extractSiteByHost(sites, "service-2.brand-3"))
	assert.Equal(t, "default", extractSiteByHost(sites, "service-1.default"))
}

func TestSynchronizer_extractSiteByHost_NoDefaultSite(t *testing.T) {
	sites := domain.Sites{
		"brand-3": domain.Services{
			"service-1": domain.AddressList{
				"service-1.default",
			},
			"service-2": domain.AddressList{
				"service-2.brand-3",
			},
		},
	}
	assert.Equal(t, "brand-3", extractSiteByHost(sites, "service-2.brand-3"))
	assert.Equal(t, "brand-3", extractSiteByHost(sites, "service-1.default"))
}

func TestSynchronizer_extractSiteByHost_NoHost(t *testing.T) {
	sites := domain.Sites{
		"brand-3": domain.Services{
			"service-1": domain.AddressList{
				"service-1.default",
			},
			"service-2": domain.AddressList{
				"service-2.brand-3",
			},
		},
	}
	assert.Equal(t, "", extractSiteByHost(sites, "service-2.brand-5"))
	assert.Equal(t, "", extractSiteByHost(sites, "service-1.brand-5"))
}

func TestSynchronizer_extractSiteByHost_NoSites(t *testing.T) {
	sites := domain.Sites{}
	assert.Equal(t, "", extractSiteByHost(sites, "service-2.brand-3"))
}
