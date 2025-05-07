package tm

import (
	"context"
	"errors"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"testing"
)

const (
	tenant1 = "tenant_1"
	tenant2 = "tenant_2"
	tenant3 = "tenant_3"
)

func TestClient_GetActiveTenantsCache(t *testing.T) {
	ctx := context.Background()
	client := Client{}
	tenants := client.GetActiveTenantsCache(ctx)
	assert.Empty(t, tenants)
	fillActiveClientsCache(&client)
	assert.Equal(t, 3, len(client.GetActiveTenantsCache(ctx)))
}

func TestClient_UpdateActiveTenantsCache(t *testing.T) {
	ctx := context.Background()
	client := Client{}
	fillActiveClientsCache(&client)
	tenantsForUpdate := []Tenant{
		{ExternalId: tenant1, Status: StatusActive},
		{ExternalId: "new-tenant", Status: StatusActive},
		{ExternalId: "another-new_tenant", Status: "not-active"},
	}
	client.UpdateActiveTenantsCache(ctx, tenantsForUpdate)
	assert.Equal(t, 4, len(client.GetActiveTenantsCache(ctx)))
}

func TestClient_TenantWithSuspendedStatus(t *testing.T) {
	ctx := context.Background()
	client := Client{}
	fillActiveClientsCache(&client)
	tenantsForUpdate := []Tenant{
		{ExternalId: tenant1, Status: StatusSuspended},
	}
	client.UpdateActiveTenantsCache(ctx, tenantsForUpdate)
	assert.Equal(t, 2, len(client.GetActiveTenantsCache(ctx)))
}

func TestClient_DeleteFromActiveTenantsCache(t *testing.T) {
	ctx := context.Background()
	client := Client{}
	fillActiveClientsCache(&client)
	tenantsForDeletion := []Tenant{
		{ExternalId: tenant1, Status: StatusActive},
	}
	client.DeleteFromActiveTenantsCache(ctx, tenantsForDeletion)
	assert.Equal(t, 2, len(client.GetActiveTenantsCache(ctx)))
}

func fillActiveClientsCache(client *Client) {
	client.activeTenantsCache.Store(tenant1, Tenant{ExternalId: tenant1, Status: StatusActive})
	client.activeTenantsCache.Store(tenant2, Tenant{ExternalId: tenant2, Status: StatusActive})
	client.activeTenantsCache.Store(tenant3, Tenant{ExternalId: tenant3, Status: StatusActive})
}

func TestPerformRequestOnErr(t *testing.T) {
	doRetryRequestFunc = func(context.Context, string, string, []byte, logging.Logger) (*fasthttp.Response, error) {
		return nil, errors.New("some-network-error")
	}
	resp, err := performRequest(context.Background(), "http://a:8080", fasthttp.MethodGet, nil)
	assert.NotNil(t, err)
	assert.Nil(t, resp)
}

func TestPerformRequestFine(t *testing.T) {
	doRetryRequestFunc = func(context.Context, string, string, []byte, logging.Logger) (*fasthttp.Response, error) {
		resp := fasthttp.AcquireResponse()
		resp.SetStatusCode(fasthttp.StatusNotFound)
		return resp, nil
	}
	resp, err := performRequest(context.Background(), "http://a:8080", fasthttp.MethodGet, nil)
	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, fasthttp.StatusNotFound, resp.StatusCode())
}

func TestClientGetAllTenantsWithoutStatusQuery(t *testing.T) {
	doRetryRequestFunc = func(_ context.Context, _, url string, _ []byte, _ logging.Logger) (*fasthttp.Response, error) {
		assert.Equal(t, tenantManagerPath+tenantsApiPath, url)
		resp := fasthttp.AcquireResponse()
		resp.SetStatusCode(fasthttp.StatusOK)
		resp.SetBodyString("[{\"objectId\": \"test-id\"}]")
		return resp, nil
	}
	client := Client{}
	tenants, err := client.GetAllTenantsByStatus(context.Background(), "")
	assert.Nil(t, err)
	assert.NotNil(t, tenants)
	assert.Len(t, *tenants, 1)
	assert.Equal(t, "test-id", (*tenants)[0].ObjectId)
}

func TestClientGetAllTenantsWithStatusQuery(t *testing.T) {
	doRetryRequestFunc = func(_ context.Context, _, url string, _ []byte, _ logging.Logger) (*fasthttp.Response, error) {
		assert.Equal(t, tenantManagerPath+tenantsApiPath+"?search=status="+StatusActive, url)
		resp := fasthttp.AcquireResponse()
		resp.SetStatusCode(fasthttp.StatusOK)
		resp.SetBodyString("[{\"objectId\": \"test-id\", \"status\": \"ACTIVE\"}]")
		return resp, nil
	}
	client := Client{}
	tenants, err := client.GetAllTenantsByStatus(context.Background(), StatusActive)
	assert.Nil(t, err)
	assert.NotNil(t, tenants)
	assert.Len(t, *tenants, 1)
	assert.Equal(t, "test-id", (*tenants)[0].ObjectId)
	assert.Equal(t, StatusActive, (*tenants)[0].Status)
}

func TestClientGetTenantByExternalIdSuccess(t *testing.T) {
	externalId := "a03-afc-gbnvc"
	doRetryRequestFunc = func(_ context.Context, _, url string, _ []byte, _ logging.Logger) (*fasthttp.Response, error) {
		assert.Equal(t, tenantManagerPath+tenantsApiPath+"/"+externalId, url)
		resp := fasthttp.AcquireResponse()
		resp.SetStatusCode(fasthttp.StatusOK)
		resp.SetBodyString("{\"externalId\": \"" + externalId + "\"}")
		return resp, nil
	}
	client := Client{}
	tenant, err := client.GetTenantByExternalId(context.Background(), externalId)
	assert.Nil(t, err)
	assert.NotNil(t, tenant)
	assert.Equal(t, externalId, tenant.ExternalId)
}
