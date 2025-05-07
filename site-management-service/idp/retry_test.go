package idp_test

import (
	"context"
	"github.com/go-errors/errors"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/idp"
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
	"time"
)

var log = logging.GetLogger("idp_retry_test")

func TestClientWithRetry_PostURI(t *testing.T) {
	wg := &sync.WaitGroup{}
	idpClientMock := newIDPClientMock(func(ctx context.Context, request idp.URIRequest) error {
		wg.Done()
		return nil
	})
	retryClient := idp.NewClientWithRetry(idpClientMock)
	wg.Add(1)
	err := retryClient.PostURI(context.Background(), idp.URIRequest{
		Namespace: "default",
		Tenants: []idp.Tenant{
			{Id: "tenant1", URLs: []string{"url1", "url2", "url3"}},
		},
		CloudCommon: idp.CloudCommon{URLs: []string{"url5", "url6", "url7"}},
	})
	if !assert.Nil(t, err) {
		t.FailNow()
	}
	wg.Wait()
	err = retryClient.PostURI(context.Background(), idp.URIRequest{
		Namespace: "default",
		Tenants: []idp.Tenant{
			{Id: "tenant1", URLs: []string{"url3", "url2", "url1"}},
		},
		CloudCommon: idp.CloudCommon{URLs: []string{"url7", "url6", "url5"}},
	})
	assert.Equal(t, 1, idpClientMock.postURICalls)
}

func TestClientWithRetry_PostURI_SingleFail(t *testing.T) {
	attemptNumVal := 0
	attemptNum := &attemptNumVal
	idpClientMock := newIDPClientMock(func(ctx context.Context, request idp.URIRequest) error {
		*attemptNum++
		log.InfoC(ctx, "Performing attempt %v of route posting", *attemptNum)
		if *attemptNum == 1 {
			return errors.New("1st attempt fails in this test case")
		}
		return nil
	})
	retryClient := idp.NewClientWithRetry(idpClientMock)

	err := retryClient.PostURI(context.Background(), idp.URIRequest{
		Namespace: "default",
		Tenants: []idp.Tenant{
			{Id: "tenant1", URLs: []string{"url1", "url2", "url3"}},
		},
		CloudCommon: idp.CloudCommon{URLs: []string{"url5", "url6", "url7"}},
	})
	if !assert.Nil(t, err) {
		t.FailNow()
	}
	time.Sleep(15 * time.Second) // wait 15 secs to make sure that no more calls performed
	assert.Equal(t, 2, *attemptNum)
	assert.Equal(t, 2, idpClientMock.postURICalls)
}
