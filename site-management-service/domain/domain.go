package domain

import (
	"fmt"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/paasMediationClient/domain"
	"hash/crc32"
	"net/url"
	"sort"
	"strings"
)

const (
	Active                 = "ACTIVE"
	IdentityProviderId     = "identity-provider"
	PublicGatewayServiceId = "public-gateway-service"

	httpProtocol  = "http://"
	httpsProtocol = "https://"
)

var logger logging.Logger

func init() {
	logger = logging.GetLogger("domain")
	logger.Info("synchronizer logger was initiated")
}

type (
	Init struct {
		Initialized bool `bson:"init"`
	}

	ValidationInfo struct {
		Valid  bool   `json:"valid"`
		Reason string `json:"reason"`
	}

	ValidationResult map[string]map[string]ValidationInfo

	Address string

	AddressList []Address

	Services map[string]AddressList

	Sites map[string]Services

	TenantDns struct {
		TenantId    string   `bson:"tenantId" json:"tenantId" bun:",pk"`
		TenantAdmin string   `bson:"tenantAdmin" json:"tenantAdmin"`
		Sites       Sites    `bson:"sites" json:"sites"`
		Active      bool     `bson:"active" json:"active"`
		Namespaces  []string `bson:"namespaces" json:"namespaces" bun:",array"`
		DomainName  string   `bson:"domainName" json:"domainName"`
		ServiceName string   `bson:"serviceName" json:"serviceName"`
		TenantName  string   `bson:"tenantName" json:"tenantName"`
		Removed     bool     `bson:"removed" json:"-"`
	}

	CompositeNamespace struct {
		Child     *CompositeNamespace
		Namespace string
	}

	// SortableByTenantId implements sort.Interface for []TenantDns based on the TenantId field.
	SortableByTenantId []TenantDns

	// SortableByTenantId implements sort.Interface for []AddressList based on the address.
	SortableAddressList AddressList

	TenantDataList struct {
		Tenants *[]*TenantData
	}

	TenantData struct {
		TenantId      *string                `json:"tenantId"`
		TenantName    string                 `json:"name"`
		Protocol      string                 `json:"protocol"`
		Site          string                 `json:"site"`
		IgnoreMissing bool                   `json:"ignoreMissing"`
		Routes        []domain.CustomService `json:"routes"`
	}

	TenantWithCustomServices struct {
	}

	Realm struct {
		RealmId string   `json:"tenant"`
		Routes  []string `json:"routes"`
	}

	Realms struct {
		Tenants      []Realm  `json:"tenants"`
		CommonRoutes []string `json:"cloud-common"`
	}

	ServiceRegistration struct {
		OriginalService           string            `json:"originalService"`
		Port                      Port              `json:"port"`
		VirtualService            string            `json:"virtualService"`
		VirtualServiceAnnotations map[string]string `json:"virtualServiceAnnotations"`
		Hostname                  string            `json:"hostname"`
	}

	Port struct {
		ServicePort int32  `json:"originalServicePort"`
		PortName    string `json:"originalServicePortName"`
	}
)

func (t *TenantDns) String() string {
	return fmt.Sprintf("TenantDns{tenantId=%s,tenantAdmin=%s,sites=%s,active=%t,namespaces=%s,domainName=%s,serviceName=%s,tenantName=%s,removed=%t}",
		t.TenantId, t.TenantAdmin, t.Sites, t.Active, t.Namespaces, t.DomainName, t.ServiceName, t.TenantName, t.Removed)
}

func NewValidationResult() ValidationResult {
	return make(map[string]map[string]ValidationInfo)
}

func (a SortableByTenantId) Len() int           { return len(a) }
func (a SortableByTenantId) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a SortableByTenantId) Less(i, j int) bool { return a[i].TenantId < a[j].TenantId }

func (a SortableAddressList) Len() int           { return len(a) }
func (a SortableAddressList) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a SortableAddressList) Less(i, j int) bool { return a[i] < a[j] }

func (s ServiceRegistration) ToService() *domain.Service {
	annotations := map[string]string{
		"qubership.cloud/tenant.service.alias.prefix": s.VirtualService,
		"qubership.cloud/tenant.service.show.name":    strings.Title(strings.ReplaceAll(s.VirtualService, "-", " ")),
		"qubership.cloud/tenant.service.type":         "virtual",
	}
	for k, v := range s.VirtualServiceAnnotations {
		annotations[k] = v
	}
	if s.Port == (Port{}) {
		s.Port.PortName = "web"
		s.Port.ServicePort = 8080
	}
	return &domain.Service{
		Metadata: domain.Metadata{
			Name:        s.VirtualService,
			Annotations: annotations,
		},
		Spec: domain.ServiceSpec{
			Selector: map[string]string{"name": s.OriginalService},
			Type:     "ClusterIP",
			Ports: []domain.Port{
				{
					Port:       s.Port.ServicePort,
					Name:       s.Port.PortName,
					TargetPort: s.Port.ServicePort,
					Protocol:   "TCP",
				},
			},
		},
	}
}

func (s ServiceRegistration) ToRoute(platformHost, namespace string) *domain.Route {
	var host Address
	if s.Hostname != "" {
		host = Address(s.Hostname)
	} else {
		host = Address(fmt.Sprintf("%s-%s.%s", s.VirtualService, namespace, platformHost))
	}
	return &domain.Route{
		Metadata: domain.Metadata{
			Name: s.VirtualService,
			Annotations: map[string]string{
				"qubership.cloud/tenant.service.tenant.id": "GENERAL",
				"qubership.cloud/tenant.service.show.name": strings.Title(strings.ReplaceAll(s.VirtualService, "-", " ")),
				"qubership.cloud/tenant.service.id":        s.VirtualService,
				"qubership.cloud/tenant.service.type":      "virtual",
			},
		},
		Spec: domain.RouteSpec{
			Host:    host.Host(),
			Service: domain.Target{Name: s.VirtualService},
			Port: domain.RoutePort{
				TargetPort: s.Port.ServicePort,
			},
		},
	}
}

func (s ServiceRegistration) IsRouteValid(route *domain.Route, namespace string) (msg string, valid bool) {
	FQDN := route.Spec.Host
	if FQDN == "" {
		routeNameWithNamespace := route.Metadata.Name + namespace
		if !s.isHostnameLengthCorrect(routeNameWithNamespace) {
			return fmt.Sprintf("Hostname %s is too long (more than 63 characters)", routeNameWithNamespace), false
		}
	} else {
		var hostName string
		if i := strings.Index(FQDN, "."); i != -1 {
			hostName = FQDN[:i]
		}
		if !s.isHostnameLengthCorrect(hostName) {
			return fmt.Sprintf("Hostname %s is too long (more than 63 characters)", hostName), false
		}
		if !s.isFQDNLengthCorrect(FQDN) {
			return fmt.Sprintf("FQDN %s is too long (more than 255 characters)", FQDN), false
		}
	}
	return "", true
}

func (s ServiceRegistration) isHostnameLengthCorrect(host string) bool {
	return len(host) < 64
}

func (s ServiceRegistration) isFQDNLengthCorrect(host string) bool {
	return len(host) < 256
}

func (s Services) clone() Services {
	clone := Services{}

	for service, addresses := range s {
		for _, address := range addresses {
			clone[service] = append(clone[service], address)
		}
	}
	return clone
}

func (t TenantDns) AppendToRoutes(routes *[]domain.Route) *[]domain.Route {
	for _, siteServices := range t.Sites {
		for service, addresses := range siteServices {
			for _, address := range addresses {
				*routes = append(*routes, *t.ToRoute(service, address))
			}
		}
	}
	return routes
}

func (t TenantDns) ToRoute(serviceName string, address Address) *domain.Route {
	// TODO: check that requested serviceName and address exists in this struct
	return &domain.Route{
		Metadata: domain.Metadata{
			Name:        t.GenerateUniqueNameForRoute(serviceName, address),
			Annotations: map[string]string{"qubership.cloud/tenant.service.tenant.id": t.TenantId},
		},
		Spec: domain.RouteSpec{
			Host:    address.Host(),
			Service: domain.Target{Name: serviceName},
		},
	}
}

func (t TenantDns) GenerateUniqueNameForRoute(serviceName string, address Address) string {
	crc32q := crc32.MakeTable(0xD5828281)
	return fmt.Sprintf("%s-%s-%08x", serviceName, t.TenantId, crc32.Checksum([]byte(address), crc32q))
}

func (t *TenantDns) FlattenAddressesToHosts() {
	// need to convert any url with scheme and path to host only string
	for siteName, servicesMap := range t.Sites {
		for service, addresses := range servicesMap {
			addressesAsHosts := AddressList{}
			for _, addr := range addresses {
				addressesAsHosts = append(addressesAsHosts, Address(addr.Host()))
			}
			servicesMap[service] = addressesAsHosts
		}
		t.Sites[siteName] = servicesMap
	}
}

func FromRoutes(routes *[]domain.Route) *[]TenantDns {
	tmp := make(map[string]TenantDns)
	for _, route := range *routes {
		tenantId := route.Metadata.Annotations["qubership.cloud/tenant.service.tenant.id"]
		tr, ok := tmp[tenantId]
		if !ok {
			tr = TenantDns{Sites: Sites{}}
			tr.TenantId = tenantId

			tmp[tenantId] = tr
		}
		// TODO support sites annotation
		siteId := "default"
		services, ok := tr.Sites[siteId]
		if !ok {
			services = Services{}
			tr.Sites[siteId] = services
		}

		serviceName := route.Spec.Service.Name
		logger.Infof("ServiceName FromRoutes %s for tenantId %s", serviceName, tenantId)
		addresses, ok := services[serviceName]
		if !ok {
			addresses = AddressList{}
		}

		services[serviceName] = append(addresses,
			ConcatAddress(route.Spec.Host, route.Metadata.Annotations["qubership.cloud/tenant.service.url.suffix"]))
	}

	// transfer map to list
	tenantDnses := make([]TenantDns, 0)
	for _, d := range tmp {
		d.Active = true
		tenantDnses = append(tenantDnses, d)
	}

	// sort tenantDns and its inner array fields
	SortTenantDns(tenantDnses)

	return &tenantDnses
}

func SortTenantDns(tenantDnses []TenantDns) {
	// sort tenantDns array
	sort.Sort(SortableByTenantId(tenantDnses))

	// loop over each tenantDns and sort inner arrays
	for _, tenantDns := range tenantDnses {
		for _, services := range tenantDns.Sites {
			for _, addressList := range services {
				sort.Sort(SortableAddressList(addressList))
			}
		}
	}
}

func ConcatAddress(host, suffix string) Address {
	if suffix != "" {
		return Address(fmt.Sprintf("%s/%s", host, suffix))
	} else {
		return Address(host)
	}
}

func (a Address) Host() string {
	addr := string(a)
	if !strings.HasPrefix(addr, httpProtocol) && !strings.HasPrefix(addr, httpsProtocol) {
		addr = httpsProtocol + addr
	}
	result, err := url.Parse(addr)
	if err != nil {
		fmt.Println(err.Error())
		return ""
	} else {
		return result.Host
	}
}

func (a Address) Path() string {
	addr := string(a)
	if !strings.HasPrefix(addr, httpProtocol) || !strings.HasPrefix(addr, httpsProtocol) {
		addr = httpsProtocol + addr
	}
	result, err := url.ParseRequestURI(addr)
	if err != nil {
		fmt.Println(err.Error())
		return ""
	} else {
		return result.Path
	}
}

func MergeDatabaseSchemeWithGeneralRoutes(dbScheme *TenantDns, generalRoutes *[]domain.Route) *TenantDns {
	generalScheme := fromGeneralRoutes(*dbScheme, generalRoutes)

	for dbSite, dbServices := range (*dbScheme).Sites {
		for dbService, dbAddresses := range dbServices {
			generalScheme.Sites[dbSite][dbService] = dbAddresses
		}
	}
	return generalScheme
}

func FilterBySite(tenant *TenantDns, site string) *TenantDns {
	for tenantSite := range (*tenant).Sites {
		if tenantSite != site {
			delete(tenant.Sites, tenantSite)
		}
	}
	return tenant
}

func fromGeneralRoutes(scheme TenantDns, generalRoutes *[]domain.Route) *TenantDns {
	generalScheme := TenantDns{
		Active:      scheme.Active,
		TenantId:    scheme.TenantId,
		TenantAdmin: scheme.TenantAdmin,
		Sites:       Sites{},
		Namespaces:  scheme.Namespaces,
		DomainName:  scheme.DomainName,
		TenantName:  scheme.TenantName,
		ServiceName: scheme.ServiceName,
	}
	services := routesToServices(generalRoutes)

	for site := range scheme.Sites {

		generalScheme.Sites[site] = services.clone()
	}
	return &generalScheme
}

func routesToServices(routes *[]domain.Route) Services {
	var services = Services{}

	for _, route := range *routes {
		serviceName := route.Spec.Service.Name
		addresses := append(AddressList{}, Address(route.Spec.Host))
		services[serviceName] = addresses
	}
	return services
}
