package idp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-errors/errors"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/http/rest"
	"github.com/valyala/fasthttp"
)

//go:generate mockgen -source=../http/rest/client.go -destination=mocks_test.go -package=idp_test

func UnexpectedResponseCode(code int) error {
	return fmt.Errorf("got response with unexpected code from identity-provider '%d'", code)
}

const postURIPath = "/auth/actions/urls"

type ClientImpl struct {
	idpURL     string
	restClient rest.Client
	log        logging.Logger
}

func NewClient(idpURL string, rest rest.Client, logger logging.Logger) *ClientImpl {
	return &ClientImpl{
		idpURL:     idpURL,
		restClient: rest,
		log:        logger,
	}
}

func (c *ClientImpl) CheckPostURIFeature(ctx context.Context) (bool, error) {
	resp, err := c.restClient.SendRequest(ctx, c.idpURL+postURIPath, fasthttp.MethodOptions, nil)
	if err != nil {
		return false, errors.Wrap(err, 0)
	}
	defer fasthttp.ReleaseResponse(resp)
	if resp.StatusCode() == fasthttp.StatusOK {
		return true, nil
	} else {
		if resp.StatusCode() == fasthttp.StatusNotFound {
			return false, nil
		}
		return false, errors.New(fmt.Errorf("got unexpected code from IDP: %d", resp.StatusCode()))
	}
}

func (c *ClientImpl) PostURI(ctx context.Context, request URIRequest) error {
	// We ignore error here because we test (TestMarshalURIRequest) during build phase
	body, _ := json.Marshal(request)
	c.log.InfoC(ctx, "Sending request to IDP. Body %s", string(body))
	resp, err := c.restClient.SendRequest(ctx, c.idpURL+postURIPath, fasthttp.MethodPost, body)
	if err != nil {
		return errors.Wrap(err, 0)
	}
	defer fasthttp.ReleaseResponse(resp)
	if resp.StatusCode() < 200 || resp.StatusCode() > 299 {
		return errors.New(UnexpectedResponseCode(resp.StatusCode()))
	}
	return nil
}
