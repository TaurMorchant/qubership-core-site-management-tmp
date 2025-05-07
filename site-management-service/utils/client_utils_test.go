package utils

import (
	"context"
	"errors"
	"github.com/netcracker/qubership-core-lib-go/v3/configloader"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-lib-go/v3/security"
	"github.com/netcracker/qubership-core-lib-go/v3/serviceloader"
	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	serviceloader.Register(1, &security.DummyToken{})
	os.Exit(m.Run())
}

func TestConstructRequestErrOnM2mErr(t *testing.T) {
	getConfig().getToken = func(context.Context) (string, error) {
		return "", errors.New("m2m err")
	}
	req, err := constructRequest(context.Background(), fasthttp.MethodGet, "http://aaa:8080", nil, logging.GetLogger(""))
	assert.NotNil(t, err)
	assert.NotNil(t, req)
}

func TestConstructRequestFine(t *testing.T) {
	getConfig().getToken = func(context.Context) (string, error) {
		return "m2m", nil
	}
	req, err := constructRequest(context.Background(), fasthttp.MethodGet, "http://aaa:8080", nil, logging.GetLogger(""))
	assert.Nil(t, err)
	assert.NotNil(t, req)
	assert.Equal(t, "Bearer m2m", string(req.Header.Peek("Authorization")))
	assert.Equal(t, req.Header.Method(), []byte(fasthttp.MethodGet))
	assert.Equal(t, req.RequestURI(), []byte("http://aaa:8080"))
}

func TestDoRetryRequestSecondTryFine(t *testing.T) {
	configloader.Init()
	getConfig().getToken = func(context.Context) (string, error) {
		return "m2m", nil
	}
	tryNum := 1
	getConfig().do = func(req *fasthttp.Request, resp *fasthttp.Response) error {
		if tryNum == 2 {
			resp.SetStatusCode(fasthttp.StatusOK)
			resp.SetBody([]byte("BodyOK"))
			return nil
		}
		tryNum++
		return errors.New("first error on call")
	}

	resp, err := DoRetryRequest(context.Background(), "", "", nil, logging.GetLogger(""))
	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 2, tryNum)
	assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())
	assert.Equal(t, []byte("BodyOK"), resp.Body())
}

func TestDoRequestFine(t *testing.T) {
	getConfig().getToken = func(context.Context) (string, error) {
		return "m2m", nil
	}
	getConfig().do = func(req *fasthttp.Request, resp *fasthttp.Response) error {
		resp.SetStatusCode(fasthttp.StatusOK)
		resp.SetBody([]byte("BodyOK"))
		return nil
	}

	resp, err := DoRequest(context.Background(), "", "", nil, logging.GetLogger(""))
	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, []byte("BodyOK"), resp.Body())
}
