package idp_test

import (
	"context"
	"github.com/go-errors/errors"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/idp"
	"reflect"
	"testing"
)

type idpClientMock struct {
	postURICalls int
	postURIFunc  func(context.Context, idp.URIRequest) error
}

func newIDPClientMock(postURIFunc func(ctx context.Context, request idp.URIRequest) error) *idpClientMock {
	mock := idpClientMock{
		postURICalls: 0,
	}
	mock.setPostURIFunc(postURIFunc)
	return &mock
}

func (i *idpClientMock) CheckPostURIFeature(ctx context.Context) (bool, error) {
	panic("implement me")
}

func (i *idpClientMock) setPostURIFunc(postURIFunc func(ctx context.Context, request idp.URIRequest) error) {
	i.postURIFunc = func(ctx context.Context, request idp.URIRequest) error {
		i.postURICalls++
		return postURIFunc(ctx, request)
	}
}

func (i *idpClientMock) PostURI(ctx context.Context, request idp.URIRequest) error {
	return i.postURIFunc(ctx, request)
}

func TestFacade_SetRedirectURIs(t *testing.T) {
	type fields struct {
		namespace string
		idpClient idp.Client
		logger    logging.Logger
	}
	type args struct {
		ctx        context.Context
		tenantURIs map[string][]string
		commonURIs []string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "Success case",
			fields: fields{idpClient: newIDPClientMock(func(ctx context.Context, request idp.URIRequest) error {
				expectedRequest := idp.URIRequest{
					Namespace: "default",
					Tenants: []idp.Tenant{
						{Id: "tenantId1", URLs: []string{"1", "2", "3"}},
					},
					CloudCommon: idp.CloudCommon{URLs: []string{"4", "5", "6"}},
				}
				if !reflect.DeepEqual(request, expectedRequest) {
					t.Fatalf("Expected %v \n but got %v", expectedRequest, request)
				}
				return nil
			}),
				logger:    logging.GetLogger("test-idp-facade"),
				namespace: "default",
			},
			args: args{
				ctx:        nil,
				tenantURIs: map[string][]string{"tenantId1": {"1", "2", "3", ""}},
				commonURIs: []string{"4", "5", "6", ""},
			},
			wantErr: false,
		},
		{
			name: "Error case",
			fields: fields{idpClient: newIDPClientMock(func(ctx context.Context, request idp.URIRequest) error {
				return errors.New("some errors")
			}),
				logger:    logging.GetLogger("test-idp-facade"),
				namespace: "default",
			},
			args: args{
				ctx:        nil,
				tenantURIs: nil,
				commonURIs: nil,
			},
			wantErr: true,
		},
		{
			name: "Request must not contain nil values",
			fields: fields{idpClient: newIDPClientMock(func(ctx context.Context, request idp.URIRequest) error {
				if request.Namespace == "" {
					t.Error("Namespace must be not empty")
				}
				if request.Tenants == nil {
					t.Error("Tenants in request must be empty but got nil")
				}
				if request.CloudCommon.URLs == nil {
					t.Error("Cloud-common URLs in request must be empty but got nil")
				}
				for _, tenants := range request.Tenants {
					if tenants.URLs == nil {
						t.Error("Tenant URLs in request must be empty but got nil")
					}
				}
				return nil
			}), logger: logging.GetLogger("test-idp-facade"), namespace: "default"},
			args: args{
				ctx:        nil,
				tenantURIs: nil,
				commonURIs: nil,
			},
			wantErr: false,
		},
		{
			name: "Request must not contain nil values. Tenant check",
			fields: fields{idpClient: newIDPClientMock(func(ctx context.Context, request idp.URIRequest) error {
				if request.Namespace == "" {
					t.Error("Namespace must be not empty")
				}
				if request.Tenants == nil {
					t.Error("Tenants in request must be empty but got nil")
				}
				if request.CloudCommon.URLs == nil {
					t.Error("Cloud-common URLs in request must be empty but got nil")
				}
				for _, tenants := range request.Tenants {
					if tenants.URLs == nil {
						t.Error("Tenant URLs in request must be empty but got nil")
					}
				}
				return nil
			}), logger: logging.GetLogger("test-idp-facade"), namespace: "default"},
			args: args{
				ctx:        nil,
				tenantURIs: map[string][]string{"tenant1": nil},
				commonURIs: nil,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := idp.NewFacade(tt.fields.namespace, tt.fields.idpClient, tt.fields.logger)
			if err := c.SetRedirectURIs(tt.args.ctx, tt.args.tenantURIs, tt.args.commonURIs); (err != nil) != tt.wantErr {
				t.Errorf("SetRedirectURIs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
