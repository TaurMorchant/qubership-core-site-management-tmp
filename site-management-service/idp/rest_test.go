package idp_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/golang/mock/gomock"
	logging "github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/idp"
	"github.com/valyala/fasthttp"
	"testing"
)

// This test is necessary because it helps to prevent us having issues with marshaling URIRequest structure
// For now URIRequest is composed of simple data types but if it starts having some unsupported types
// for example (chan, infinity number etc.) or the same but hiding under interface{}
func TestMarshalURIRequest(t *testing.T) {
	_, err := json.Marshal(idp.URIRequest{
		Tenants:     nil,
		CloudCommon: idp.CloudCommon{},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestClientImpl_PostURI_RESTClientError(t *testing.T) {
	ctrl := gomock.NewController(t)

	restClientMock := NewMockClient(ctrl)
	restClientMock.
		EXPECT().
		SendRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, string, []byte) (*fasthttp.Response, error) {
			return nil, errors.New("some error")
		}).
		Times(1)

	client := idp.NewClient("http://identity-provider:8080", restClientMock, logging.GetLogger("test-idp-client"))
	err := client.PostURI(nil, idp.URIRequest{})
	if err == nil {
		t.Fatal("Expected error but got nil")
	}
}

func TestClientImpl_PostURI_RESTClientReturn404(t *testing.T) {
	ctrl := gomock.NewController(t)

	restClientMock := NewMockClient(ctrl)
	restClientMock.
		EXPECT().
		SendRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, string, []byte) (*fasthttp.Response, error) {
			resp := fasthttp.AcquireResponse()
			resp.SetStatusCode(fasthttp.StatusNotFound)
			resp.SetBody(nil)
			return resp, nil
		}).
		Times(1)

	client := idp.NewClient("http://identity-provider:8080", restClientMock, logging.GetLogger("test-idp-client"))
	err := client.PostURI(nil, idp.URIRequest{})
	if err == nil {
		t.Fatal("Expected error but got nil")
	}
}

func TestClientImpl_PostURI_RESTClientReturnOk(t *testing.T) {
	ctrl := gomock.NewController(t)

	restClientMock := NewMockClient(ctrl)
	restClientMock.
		EXPECT().
		SendRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, string, []byte) (*fasthttp.Response, error) {
			resp := fasthttp.AcquireResponse()
			resp.SetStatusCode(fasthttp.StatusOK)
			resp.SetBody(nil)
			return resp, nil
		}).
		Times(1)

	client := idp.NewClient("http://identity-provider:8080", restClientMock, logging.GetLogger("test-idp-client"))
	err := client.PostURI(nil, idp.URIRequest{})
	if err != nil {
		t.Fatal("Got error but expected nil")
	}
}
