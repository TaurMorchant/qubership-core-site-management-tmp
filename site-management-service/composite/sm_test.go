package composite

import (
	"context"
	"errors"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/domain"
	assert2 "github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"net/http"
	"testing"
)

type recordedRequest struct {
	url    string
	method string
	body   string
}

type mockRestClient struct {
	assert  *assert2.Assertions
	handler func(req recordedRequest) (*fasthttp.Response, error)
}

func (mock *mockRestClient) SendRequest(ctx context.Context, url string, method string, body []byte) (*fasthttp.Response, error) {
	strBody := ""
	if body != nil {
		strBody = string(body)
	}

	request := recordedRequest{
		url:    url,
		method: method,
		body:   strBody,
	}

	logger.InfoC(ctx, "MockRestClient got request: %+v", request)
	return mock.handler(request)
}

func TestGetIdpRoute_Success(t *testing.T) {
	logCtx := context.Background()
	assert := assert2.New(t)
	mockRestClient := mockRestClient{
		assert: assert,
		handler: func(req recordedRequest) (*fasthttp.Response, error) {
			assert.Equal("http://site-management.fake-baseline:8080/api/v1/identity-provider-route?tenantId=test-tenant&protocol=http&site=tenant.com&ignoreMissing=true", req.url)
			assert.Equal(http.MethodGet, req.method)
			resp := fasthttp.AcquireResponse()
			resp.SetStatusCode(fasthttp.StatusOK)
			resp.SetBody([]byte(`[
    {
        "id": "identity-provider",
        "name": "Identity Provider",
        "url": "https://public-gateway-fake-baseline.test-cloud.qubership.org/",
        "description": "URL to access Identity Provider API"
    }
]`))
			return resp, nil
		},
	}
	baselineSM := NewBaselineSM("http://site-management.fake-baseline:8080", &mockRestClient)

	routes, err := baselineSM.GetIdpRoute(logCtx, "test-tenant", "http", "tenant.com", true)
	assert.Nil(err)

	assert.Equal(1, len(routes))
	assert.Equal(domain.IdentityProviderId, routes[0].Id)
	assert.Equal("https://public-gateway-fake-baseline.test-cloud.qubership.org/", routes[0].URL)
}

func TestGetIdpRoute_Error500(t *testing.T) {
	logCtx := context.Background()
	assert := assert2.New(t)
	mockRestClient := mockRestClient{
		assert: assert,
		handler: func(req recordedRequest) (*fasthttp.Response, error) {
			assert.Equal("http://site-management.fake-baseline:8080/api/v1/identity-provider-route?tenantId=test-tenant&protocol=http&site=tenant.com&ignoreMissing=true", req.url)
			assert.Equal(http.MethodGet, req.method)
			resp := fasthttp.AcquireResponse()
			resp.SetStatusCode(fasthttp.StatusInternalServerError)
			return resp, nil
		},
	}
	baselineSM := NewBaselineSM("http://site-management.fake-baseline:8080", &mockRestClient)

	_, err := baselineSM.GetIdpRoute(logCtx, "test-tenant", "http", "tenant.com", true)
	assert.NotNil(err)
}

func TestGetIdpRoute_GoError(t *testing.T) {
	logCtx := context.Background()
	assert := assert2.New(t)
	mockRestClient := mockRestClient{
		assert: assert,
		handler: func(req recordedRequest) (*fasthttp.Response, error) {
			assert.Equal("http://site-management.fake-baseline:8080/api/v1/identity-provider-route?tenantId=test-tenant&protocol=http&site=tenant.com&ignoreMissing=true", req.url)
			assert.Equal(http.MethodGet, req.method)
			resp := fasthttp.AcquireResponse()
			resp.SetStatusCode(fasthttp.StatusOK)
			return resp, errors.New("mock_client: failed to send http request")
		},
	}
	baselineSM := NewBaselineSM("http://site-management.fake-baseline:8080", &mockRestClient)

	_, err := baselineSM.GetIdpRoute(logCtx, "test-tenant", "http", "tenant.com", true)
	assert.NotNil(err)
}
