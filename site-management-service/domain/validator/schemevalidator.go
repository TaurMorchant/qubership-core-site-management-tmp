package validator

import (
	"errors"
	"fmt"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/domain"
	urls "net/url"
	"strings"
	"unicode"
)

const (
	httpProtocol  = "http://"
	httpsProtocol = "https://"
)

var (
	domainTable = &unicode.RangeTable{
		R16: []unicode.Range16{
			{'-', '-', 1},
			{'0', '9', 1},
			{'A', 'Z', 1},
			{'a', 'z', 1},
		},
		LatinOffset: 4,
	}
)

type SchemeValidator interface {
	Check(tenant domain.TenantDns, data []domain.TenantDns, result *domain.ValidationResult)
}

type (
	urlChecker struct {
		next SchemeValidator
	}
	inTenantDuplicationChecker struct {
		next SchemeValidator
	}
	inSchemeDuplicationChecker struct {
		next SchemeValidator
	}
)

func NewSchemeValidator() SchemeValidator {
	sdc := inSchemeDuplicationChecker{
		next: nil,
	}

	tdc := inTenantDuplicationChecker{
		next: sdc,
	}

	ehc := urlChecker{
		next: tdc,
	}

	return ehc
}

func (checker urlChecker) Check(tenant domain.TenantDns, data []domain.TenantDns, result *domain.ValidationResult) {
	iterateOverTenant(tenant, data, result, validateUrl)

	if checker.next != nil {
		checker.next.Check(tenant, data, result)
	}
}

func (checker inTenantDuplicationChecker) Check(tenant domain.TenantDns, data []domain.TenantDns, result *domain.ValidationResult) {
	iterateOverTenant(tenant, data, result, validateTenantDuplications)

	if checker.next != nil {
		checker.next.Check(tenant, data, result)
	}
}

func (checker inSchemeDuplicationChecker) Check(tenant domain.TenantDns, data []domain.TenantDns, result *domain.ValidationResult) {
	iterateOverTenant(tenant, data, result, validateSchemeDuplications)

	if checker.next != nil {
		checker.next.Check(tenant, data, result)
	}
}

func iterateOverTenant(tenant domain.TenantDns, data []domain.TenantDns, result *domain.ValidationResult, function func(string, string, domain.TenantDns, []domain.TenantDns) error) {
	for site, siteServices := range tenant.Sites {
		if _, ok := (*result)[site]; !ok {
			(*result)[site] = make(map[string]domain.ValidationInfo)
		}
		for service, addresses := range siteServices {
			for _, address := range addresses {
				if err := function(service, string(address), tenant, data); err != nil {
					(*result)[site][service] = domain.ValidationInfo{
						Valid:  false,
						Reason: err.Error(),
					}
				} else {
					if _, ok := (*result)[site][service]; !ok {
						(*result)[site][service] = domain.ValidationInfo{
							Valid:  true,
							Reason: "",
						}
					}
				}
			}
		}
	}
}

func anyMatch(tenantId string, data []domain.TenantDns, predicate func(string, string) bool) error {
	for _, tenantSites := range data {
		if tenantSites.TenantId != tenantId {
			for _, siteServices := range tenantSites.Sites {
				for service, addresses := range siteServices {
					for _, address := range addresses {
						if predicate(service, string(address)) {
							return errors.New(fmt.Sprintf("Matches with url for service %s in tenant %s", service, tenantSites.TenantId))
						}
					}
				}
			}
		}
	}
	return nil
}

func validateUrl(service, url string, tenant domain.TenantDns, data []domain.TenantDns) error {
	urlToCheck := url
	if !strings.HasPrefix(url, httpProtocol) && !strings.HasPrefix(url, httpsProtocol) {
		urlToCheck = httpProtocol + urlToCheck
	}
	u, err := urls.Parse(urlToCheck)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New(fmt.Sprintf("Not a valid URL"))
	}

	parts := strings.Split(u.Host, ".")
	if len(parts) > 127 {
		return errors.New(fmt.Sprintf("Domain has too many parts"))
	}

	lastIndex := len(parts) - 1
	if !validatePart(parts[lastIndex], true) {
		return errors.New(fmt.Sprintf("Not a valid URL"))
	} else {
		for i := 0; i < lastIndex; i++ {
			if !validatePart(parts[i], false) {
				return errors.New(fmt.Sprintf("Not a valid URL"))
			}
		}
	}
	return nil
}

func validatePart(part string, isLastPart bool) bool {
	if len(part) >= 1 && len(part) <= 63 {
		for i := 0; i < len(part); i++ {
			if !unicode.Is(domainTable, rune(part[i])) {
				return false
			}
		}
		if '-' != part[0] && '-' != part[len(part)-1] {
			return !isLastPart || !unicode.IsDigit(rune(part[0]))
		} else {
			return false
		}
	} else {
		return false
	}
}

func validateTenantDuplications(service, url string, tenant domain.TenantDns, data []domain.TenantDns) error {
	urlToCheck := url
	if strings.HasPrefix(url, httpProtocol) {
		urlToCheck = strings.TrimPrefix(urlToCheck, httpProtocol)
	}
	validationResult := domain.NewValidationResult()
	iterateOverTenant(
		tenant,
		[]domain.TenantDns{},
		&validationResult,
		func(internalService string, address string, tenant domain.TenantDns, data []domain.TenantDns) error {
			if strings.HasPrefix(address, httpProtocol) {
				address = strings.TrimPrefix(address, httpProtocol)
			}
			if address == urlToCheck && internalService != service {
				return errors.New("There are duplications in tenant scheme for address " + address)
			}
			return nil
		})

	for _, site := range validationResult {
		for service, validationInfo := range site {
			if !validationInfo.Valid {
				return errors.New(fmt.Sprintf("Matches with another service %s in current tenant", service))
			}
		}
	}
	return nil
}

func validateSchemeDuplications(service, url string, tenant domain.TenantDns, data []domain.TenantDns) error {
	urlToCheck := url
	if strings.HasPrefix(url, httpProtocol) {
		urlToCheck = strings.TrimPrefix(urlToCheck, httpProtocol)
	} else if strings.HasPrefix(url, httpsProtocol) {
		urlToCheck = strings.TrimPrefix(urlToCheck, httpsProtocol)
	}

	predicate := func(internalService, address string) bool {
		if address == urlToCheck && internalService != service {
			return true
		}
		return false
	}

	err := anyMatch(tenant.TenantId, data, predicate)
	if err != nil {
		return err
	}
	return nil
}
