package domain

import (
	"fmt"
	"github.com/netcracker/qubership-core-lib-go/v3/serviceloader"
	"github.com/netcracker/qubership-core-lib-go/v3/utils"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/paasMediationClient/domain"
	"reflect"
	"testing"
)

func init() {
	serviceloader.Register(1, utils.NewResourceGroupAnnotationsMapper("qubership.cloud"))
}

func TestFromRoutes(t *testing.T) {
	routes := []domain.Route{
		{
			Metadata: domain.Metadata{
				Name: "tenant-self-service",
				Annotations: map[string]string{
					"qubership.cloud/tenant.service.show.description": "Service to manage users and services",
					"qubership.cloud/tenant.service.show.name":        "Control Panel (Personal Cabinet)",
					"qubership.cloud/tenant.service.tenant.id":        "GENERAL",
				},
			},
			Spec: domain.RouteSpec{
				Host: "tenant-self-service-cloud-catalog-system.development.openshift.sdntest.qubership.org",
				Service: domain.Target{
					Name: "tenant-self-service-fe",
				},
			},
		},
		{
			Metadata: domain.Metadata{
				Name: "tenanttestqa2",
				Annotations: map[string]string{
					"qubership.cloud/tenant.service.tenant.id":        "b416daa5-a2be-42fa-bc6f-ee3a0e805a6b",
					"qubership.cloud/tenant.service.url.suffix":       "welcome",
					"qubership.cloud/tenant.service.id":               "shopping-frontend",
					"qubership.cloud/tenant.service.show.description": "Market for your customers",
					"qubership.cloud/tenant.service.show.name":        "Shopping Catalogue",
				},
			},
			Spec: domain.RouteSpec{
				Host: "tenanttestqa2.backup.openshift.sdntest.qubership.org",
				Service: domain.Target{
					Name: "tenant-tenanttestqa2",
				},
			},
		},
		{
			Metadata: domain.Metadata{
				Name: "tenanttestqa2-www",
				Annotations: map[string]string{
					"qubership.cloud/tenant.service.id":               "shopping-frontend",
					"qubership.cloud/tenant.service.show.description": "Market for your customers",
					"qubership.cloud/tenant.service.show.name":        "Shopping Catalogue",
					"qubership.cloud/tenant.service.tenant.id":        "b416daa5-a2be-42fa-bc6f-ee3a0e805a6b",
					"qubership.cloud/tenant.service.url.suffix":       "welcome",
				},
			},
			Spec: domain.RouteSpec{
				Host: "www.tenanttestqa2.backup.openshift.sdntest.qubership.org",
				Service: domain.Target{
					Name: "tenant-tenanttestqa2",
				},
			},
		},
	}

	actual := FromRoutes(&routes)
	expected := []TenantDns{
		{
			TenantId: "GENERAL",
			Sites: Sites{
				"default": Services{
					"tenant-self-service-fe": AddressList{
						"tenant-self-service-cloud-catalog-system.development.openshift.sdntest.qubership.org",
					},
				},
			},
			Active: true,
		},
		{
			TenantId: "b416daa5-a2be-42fa-bc6f-ee3a0e805a6b",
			Sites: Sites{
				"default": Services{
					"tenant-tenanttestqa2": AddressList{
						"tenanttestqa2.backup.openshift.sdntest.qubership.org/welcome",
						"www.tenanttestqa2.backup.openshift.sdntest.qubership.org/welcome",
					},
				},
			},
			Active: true,
		},
	}

	// array in actual result are sorted so must be the expected
	SortTenantDns(expected)

	fmt.Println(actual)
	fmt.Println(expected)

	if !reflect.DeepEqual(actual, &expected) {
		t.Fatal()
	}
}

func TestTenantDns_ToRoute(t *testing.T) {
	data := TenantDns{
		TenantId: "b416daa5-a2be-42fa-bc6f-ee3a0e805a6b",
		Sites: Sites{
			"default": Services{
				"tenant-tenanttestqa2": AddressList{
					"tenanttestqa2.backup.openshift.sdntest.qubership.org/welcome",
					"www.tenanttestqa2.backup.openshift.sdntest.qubership.org/welcome",
				},
			},
		},
	}

	actual := data.ToRoute("tenant-tenanttestqa2", "http://tenanttestqa2.backup.openshift.sdntest.qubership.org/welcome")
	expected := &domain.Route{
		Metadata: domain.Metadata{
			Name: actual.Metadata.Name,
			Annotations: map[string]string{
				"qubership.cloud/tenant.service.tenant.id": "b416daa5-a2be-42fa-bc6f-ee3a0e805a6b",
			},
		},
		Spec: domain.RouteSpec{
			Host: "tenanttestqa2.backup.openshift.sdntest.qubership.org",
			Service: domain.Target{
				Name: "tenant-tenanttestqa2",
			},
		},
	}

	fmt.Println(actual.Spec.Host)
	fmt.Println(actual.Spec.Path)
	fmt.Println("-------------------------------")
	fmt.Println(actual)
	fmt.Println("-------------------------------")
	fmt.Println(expected)

	if !reflect.DeepEqual(actual, expected) {
		t.Fatal()
	}
}
