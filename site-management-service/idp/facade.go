package idp

import (
	"context"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"

	"github.com/go-errors/errors"
)

type Facade struct {
	namespace string
	log       logging.Logger
	client    Client
}

type Client interface {
	CheckPostURIFeature(context.Context) (bool, error)
	PostURI(context.Context, URIRequest) error
}

type RetryableClient interface {
	Client
	Reset()
}

type DummyRetryableClient struct {
}

func (d DummyRetryableClient) CheckPostURIFeature(_ context.Context) (bool, error) {
	return false, nil
}

func (d DummyRetryableClient) PostURI(_ context.Context, _ URIRequest) error {
	return nil
}

func (d DummyRetryableClient) Reset() {
}

type URIRequest struct {
	Namespace   string      `json:"namespace"`
	Tenants     []Tenant    `json:"tenants"`
	CloudCommon CloudCommon `json:"cloud-common"`
}

type Tenant struct {
	Id   string   `json:"id"`
	URLs []string `json:"urls"`
}

type CloudCommon struct {
	URLs []string `json:"urls"`
}

func NewFacade(namespace string, idpClient Client, logger logging.Logger) *Facade {
	client := &Facade{
		namespace: namespace,
		log:       logger,
		client:    idpClient,
	}
	return client
}

func (c *Facade) CheckPostURIFeature(ctx context.Context) (bool, error) {
	if ok, err := c.client.CheckPostURIFeature(ctx); err != nil {
		return false, errors.Wrap(err, 0)
	} else {
		return ok, nil
	}
}

func (c *Facade) SetRedirectURIs(ctx context.Context, tenantURIs map[string][]string, commonURIs []string) error {
	request := uriRequest(tenantURIs, commonURIs)
	request.Namespace = c.namespace
	c.log.DebugC(ctx, "Prepared request to POST URIs: %s", request)
	err := c.client.PostURI(ctx, request)
	if err != nil {
		return errors.Wrap(err, 0)
	}
	return nil
}

func uriRequest(tenantURIs map[string][]string, commonURIs []string) URIRequest {
	uriRequest := URIRequest{
		CloudCommon: CloudCommon{URLs: notNullSliceWithNonEmptyValues(commonURIs)},
	}
	uriRequest.Tenants = make([]Tenant, 0)
	for tenantId, uris := range tenantURIs {
		uriRequest.Tenants = append(uriRequest.Tenants, Tenant{Id: tenantId, URLs: notNullSliceWithNonEmptyValues(uris)})
	}
	return uriRequest
}

func notNullSliceWithNonEmptyValues(strings []string) []string {
	sl := make([]string, 0)

	for _, v := range strings {
		if v != "" {
			sl = append(sl, v)
		}
	}

	return sl
}
