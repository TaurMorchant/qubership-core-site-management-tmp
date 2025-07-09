package validator

import (
	"fmt"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/domain"
	"testing"
)

const (
	serviceName    = "site-management"
	anotherService = "tenant-manager"
	centralSite    = "central"
	westSite       = "west"
	defaultSite    = "default"
)

var (
	schemeValidator = NewSchemeValidator()

	tenantWithEmptyHost = domain.TenantDns{
		TenantId:    "tenantWithEmptyHost",
		TenantAdmin: "admin@example.com",
		Sites: domain.Sites{
			centralSite: domain.Services{
				serviceName: domain.AddressList{
					"",
				},
			},
			westSite: domain.Services{
				serviceName: domain.AddressList{
					"site-management.openshift.qubership.org",
				},
			},
		},
	}

	tenantWithInvalidHost = domain.TenantDns{
		TenantId:    "tenantWithInvalidHost",
		TenantAdmin: "admin@example.com",
		Sites: domain.Sites{
			centralSite: domain.Services{
				serviceName: domain.AddressList{
					"5прпр1",
				},
				anotherService: domain.AddressList{
					"_asd.com",
				},
			},
			westSite: domain.Services{
				serviceName: domain.AddressList{
					"http://site-management.openshift.sdntest.qubership.org/",
				},
			},
		},
	}

	tenantWithDuplicatedHost = domain.TenantDns{
		TenantId:    "tenantWithDuplicatedHost",
		TenantAdmin: "admin@example.com",
		Sites: domain.Sites{
			centralSite: domain.Services{
				serviceName: domain.AddressList{
					"site-management.openshift.qubership.org",
				},
			},
		},
	}

	tenantWithDuplicatedHostForAnotherService = domain.TenantDns{
		TenantId:    "tenantWithDuplicatedHostForAnotherService",
		TenantAdmin: "admin@example.com",
		Sites: domain.Sites{
			centralSite: domain.Services{
				anotherService: domain.AddressList{
					"site-management.openshift.qubership.org",
				},
			},
		},
	}

	tenantWithDuplicatedHostsForDifferentServices = domain.TenantDns{
		TenantId:    "tenantWithDuplicatedHostsForDifferentServices",
		TenantAdmin: "admin@example.com",
		Sites: domain.Sites{
			defaultSite: domain.Services{
				serviceName: domain.AddressList{
					"site-management.openshift.qubership.org",
				},
			},
			centralSite: domain.Services{
				anotherService: domain.AddressList{
					"site-management.openshift.qubership.org",
				},
			},
		},
	}

	tenantWithDuplicatedHostsForSameService = domain.TenantDns{
		TenantId:    "tenantWithDuplicatedHostsForDifferentServices",
		TenantAdmin: "admin@example.com",
		Sites: domain.Sites{
			defaultSite: domain.Services{
				serviceName: domain.AddressList{
					"site-management.openshift.qubership.org",
				},
			},
			centralSite: domain.Services{
				serviceName: domain.AddressList{
					"site-management.openshift.qubership.org",
				},
			},
		},
	}

	tenant = domain.TenantDns{
		TenantId:    "tenant",
		TenantAdmin: "admin@example.com",
		Sites: domain.Sites{
			defaultSite: domain.Services{
				serviceName: domain.AddressList{
					"site-management.openshift.qubership.org",
				},
			},
			centralSite: domain.Services{
				serviceName: domain.AddressList{
					"site-management-central.openshift.sdntest.qubership.org",
				},
			},
			westSite: domain.Services{
				serviceName: domain.AddressList{
					"site-management-west.openshift.sdntest.qubership.org",
				},
			},
		},
	}

	emptyScheme = []domain.TenantDns{}

	schemeWithDuplicatedHost = []domain.TenantDns{
		tenantWithDuplicatedHost,
	}
)

func TestInvalidHostChecker_Check(t *testing.T) {
	actual := domain.NewValidationResult()
	schemeValidator.Check(tenantWithInvalidHost, emptyScheme, &actual)

	fmt.Println(actual)

	for _, service := range actual[centralSite] {
		if service.Valid {
			t.Fatal()
		}
	}
}

func TestEmptyHostChecker_Check(t *testing.T) {
	actual := domain.NewValidationResult()
	schemeValidator.Check(tenantWithEmptyHost, emptyScheme, &actual)

	fmt.Println(actual)

	if value, ok := actual[centralSite][serviceName]; !ok || value.Valid {
		t.Fatal()
	}
}

func TestDuplicationInSchemeForSameService_Check(t *testing.T) {
	actual := domain.NewValidationResult()
	schemeValidator.Check(tenant, schemeWithDuplicatedHost, &actual)

	fmt.Println(actual)

	if value, ok := actual[defaultSite][serviceName]; !(ok && value.Valid) {
		t.Fatal()
	}
}

func TestDuplicationInSchemeForAnotherService_Check(t *testing.T) {
	actual := domain.NewValidationResult()
	schemeValidator.Check(tenantWithDuplicatedHostForAnotherService, schemeWithDuplicatedHost, &actual)

	fmt.Println(actual)

	if value, ok := actual[centralSite][anotherService]; !ok || value.Valid {
		t.Fatal()
	}
}

func TestDuplicationInTenantForSameService_Check(t *testing.T) {
	actual := domain.NewValidationResult()
	schemeValidator.Check(tenantWithDuplicatedHostsForSameService, emptyScheme, &actual)

	fmt.Println(actual)

	if value, ok := actual[defaultSite][serviceName]; !(ok && value.Valid) {
		t.Fatal()
	}
}

func TestDuplicationInTenantForAnotherService_Check(t *testing.T) {
	actual := domain.NewValidationResult()
	schemeValidator.Check(tenantWithDuplicatedHostsForDifferentServices, emptyScheme, &actual)

	fmt.Println(actual)

	if value, ok := actual[defaultSite][serviceName]; !ok || value.Valid {
		t.Fatal()
	}
}

func TestNewSchemeValidator(t *testing.T) {
	actual := domain.NewValidationResult()
	schemeValidator.Check(tenant, emptyScheme, &actual)

	fmt.Println(actual)

	for _, site := range actual {
		for _, value := range site {
			if !value.Valid {
				t.Fatal()
			}
		}
	}
}
