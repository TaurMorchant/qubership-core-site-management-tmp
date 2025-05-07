package composite

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-errors/errors"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/http/rest"
	mdomain "github.com/netcracker/qubership-core-site-management/site-management-service/v2/paasMediationClient/domain"
	"github.com/valyala/fasthttp"
)

var logger = logging.GetLogger("composite")

type BaselineSM struct {
	url        string
	restClient rest.Client
}

func NewBaselineSM(url string, restClient rest.Client) *BaselineSM {
	return &BaselineSM{url: url, restClient: restClient}
}

func (sm *BaselineSM) GetIdpRoute(logCtx context.Context, tenantId, protocol, tenantSite string, ignoreMissing bool) ([]mdomain.CustomService, error) {
	resp, err := sm.restClient.SendRequest(
		logCtx,
		sm.url+"/api/v1/identity-provider-route"+sm.fillRequestParams(tenantId, protocol, tenantSite, ignoreMissing),
		fasthttp.MethodGet,
		nil)
	if err != nil {
		logger.ErrorC(logCtx, "Failed to get annotated routes from SM in composite baseline: %v", err)
		return nil, errors.New("request to baseline SM failed: " + err.Error())
	}
	defer fasthttp.ReleaseResponse(resp)

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		logger.ErrorC(logCtx, "Request to get annotated routes from SM in composite baseline returned status: %v", resp.StatusCode())
		if len(resp.Body()) > 0 {
			logger.ErrorC(logCtx, "Response body from SM in composite baseline: %v", string(resp.Body()))
		}
		return nil, errors.New(fmt.Sprintf("baseline SM returned unexpected status: %v", resp.StatusCode()))
	}
	var result []mdomain.CustomService
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		logger.ErrorC(logCtx, "Failed to parse response body from SM in composite baseline: %v", err)
		return nil, errors.New("failed to parse response body from baseline SM" + err.Error())
	}
	return result, nil
}

func (sm *BaselineSM) fillRequestParams(tenantId, protocol, tenantSite string, ignoreMissing bool) string {
	result := &paramsString{}
	if len(tenantId) > 0 {
		result.addParam("tenantId", tenantId)
	}
	if len(protocol) > 0 {
		result.addParam("protocol", protocol)
	}
	if len(tenantSite) > 0 {
		result.addParam("site", tenantSite)
	}
	if ignoreMissing {
		result.addParam("ignoreMissing", "true")
	}
	return result.state
}

type paramsString struct {
	state string
}

func (paramStr *paramsString) addParam(name, value string) {
	if len(paramStr.state) == 0 {
		paramStr.state = "?"
	} else {
		paramStr.state += "&"
	}
	paramStr.state += fmt.Sprintf("%s=%s", name, value)
}
