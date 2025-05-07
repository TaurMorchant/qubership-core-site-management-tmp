package exceptions

import "fmt"

type (
	TenantNotFoundError struct {
		message string
	}

	TenantIsNotActiveError struct {
		message string
	}
)

func NewTenantNotFoundError(tenantId string) TenantNotFoundError {
	return TenantNotFoundError{
		message: fmt.Sprintf("Tenant %s is not present in database", tenantId),
	}
}

func (e TenantNotFoundError) Error() string {
	return e.message
}

func NewTenantIsNotActiveError(tenantId string) TenantIsNotActiveError {
	return TenantIsNotActiveError{
		message: fmt.Sprintf("Tenant %s is not in active state", tenantId),
	}
}

func (e TenantIsNotActiveError) Error() string {
	return e.message
}
