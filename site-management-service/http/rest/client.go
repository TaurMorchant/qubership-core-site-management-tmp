package rest

import (
	"context"
	"github.com/go-errors/errors"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/utils"
	"github.com/valyala/fasthttp"
)

type Client interface {
	SendRequest(ctx context.Context, url string, method string, body []byte) (*fasthttp.Response, error)
}

type SimpleClient struct {
	log logging.Logger
}

func NewClient() *SimpleClient {
	return &SimpleClient{
		log: logging.GetLogger("rest-client"),
	}
}

func (c SimpleClient) SendRequest(ctx context.Context, url string, method string, body []byte) (*fasthttp.Response, error) {
	c.log.InfoC(ctx, "Sending request with url '%s', method '%s'", url, method)
	resp, err := utils.DoRequest(ctx, method, url, body, c.log)

	if err != nil {
		c.log.ErrorC(ctx, "Failed to send secure request: %s", err)
		return nil, errors.New(err)
	}
	return resp, nil
}
