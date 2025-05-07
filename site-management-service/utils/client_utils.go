package utils

import (
	"context"
	"fmt"
	"github.com/go-errors/errors"
	"github.com/gorilla/websocket"
	"github.com/netcracker/qubership-core-lib-go/v3/configloader"
	"github.com/netcracker/qubership-core-lib-go/v3/context-propagation/ctxhelper"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-lib-go/v3/security"
	"github.com/netcracker/qubership-core-lib-go/v3/serviceloader"
	"github.com/valyala/fasthttp"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type utilConfig struct {
	getToken func(ctx context.Context) (string, error)
	do       func(req *fasthttp.Request, resp *fasthttp.Response) error
	client   *fasthttp.Client
}

var configOnce = sync.Once{}
var config *utilConfig = nil

func createConfig() {
	tokenProvider := serviceloader.MustLoad[security.TokenProvider]()
	httpclient := &fasthttp.Client{
		MaxIdleConnDuration:           30 * time.Second,
		DisableHeaderNamesNormalizing: true,
		DisablePathNormalizing:        true,
		DialDualStack:                 true,
	}
	config = &utilConfig{
		getToken: tokenProvider.GetToken,
		do:       httpclient.Do,
		client:   httpclient,
	}
}

func getConfig() *utilConfig {
	if config == nil {
		configOnce.Do(createConfig)
	}
	return config
}

func DoRetryRequest(logContext context.Context, method string, url string, data []byte, logger logging.Logger) (*fasthttp.Response, error) {
	attemptDelayStart, _ := strconv.Atoi(configloader.GetOrDefaultString("http.client.retry.attemptDelay", "2000"))
	attemptDelayStartDuration := time.Duration(attemptDelayStart) * time.Millisecond
	retryLimit, _ := strconv.Atoi(configloader.GetOrDefaultString("http.client.retry.maxAttempts", "5"))

	logger.DebugC(logContext, "Execute secure request (retryLimit: %v, retry delay: %v * n)", retryLimit, attemptDelayStart)
	errMsg := ""
	for i := 0; i < retryLimit; i++ {
		if i > 0 {
			waitInterval := attemptDelayStartDuration * time.Duration(i*i)
			logger.InfoC(logContext, "Sleep %v before retry", waitInterval)
			time.Sleep(waitInterval)
		}

		req, err := constructRequest(logContext, method, url, data, logger)
		if err != nil {
			fasthttp.ReleaseRequest(req)
			errMsg = fmt.Sprintf("Secure %s request handler to %s failed with error: %s, retrying", method, url, err)
			logger.WarnC(logContext, errMsg)
			continue
		}

		response := fasthttp.AcquireResponse()
		err = getConfig().do(req, response)
		fasthttp.ReleaseRequest(req)
		if err != nil {
			errMsg = fmt.Sprintf("Secure %s request to %s failed with error: %s, retrying", method, url, err)
			logger.WarnC(logContext, errMsg)
			fasthttp.ReleaseResponse(response)
			continue
		}
		if response.StatusCode() >= fasthttp.StatusInternalServerError {
			logger.WarnC(logContext, "Secure %s request to %s failed with 5xx http status code: %d, retrying", method, url, response.StatusCode())
			errMsg = fmt.Sprintf("Secure %s request to %s failed with 5xx http status code: %d, and body: %s retrying", method, url, response.StatusCode(), string(response.Body()))
			fasthttp.ReleaseResponse(response)
			continue
		} else {
			return response, nil
		}
	}
	return nil, errors.New(errMsg)
}

func DoRequest(logContext context.Context, method string, url string, data []byte, logger logging.Logger) (*fasthttp.Response, error) {
	req, err := constructRequest(logContext, method, url, data, logger)
	defer fasthttp.ReleaseRequest(req)
	if err != nil {
		logger.WarnC(logContext, "Secure %s request handler creation to %s failed with error: %s", method, url, err)
		return nil, err
	}
	response := fasthttp.AcquireResponse()
	err = getConfig().do(req, response)
	if err != nil {
		logger.WarnC(logContext, "Secure %s request to %s failed with error: %s, retrying", method, url, err)
		fasthttp.ReleaseResponse(response)
		return nil, err
	}

	if response.StatusCode() >= fasthttp.StatusInternalServerError {
		logger.WarnC(logContext, "Secure %s request to %s failed with 5xx http status code: %s, retrying", method, url, response.StatusCode())
		fasthttp.ReleaseResponse(response)
		return nil, errors.New(fmt.Sprintf("Secure %s request to %s failed with 5xx http status code: %v", method, url, response.StatusCode()))
	} else {
		return response, nil
	}
}

func constructRequest(ctx context.Context, method string, url string, data []byte, logger logging.Logger) (*fasthttp.Request, error) {
	req := fasthttp.AcquireRequest()
	m2mToken, err := getConfig().getToken(ctx)
	if err != nil {
		logger.ErrorC(ctx, "Can't refresh token %v", err)
		return req, err
	}
	logger.DebugC(ctx, "Request will be sent with token")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", m2mToken))
	req.Header.Add("Content-Type", "application/json")

	logger.Debugf(`Building secure request with arguments:
	method=%v, 
	url=%v`, method, url)

	req.Header.SetMethod(method)
	req.SetRequestURI(url)
	req.SetBody(data)

	if err := ctxhelper.AddSerializableContextData(ctx, req.Header.Set); err != nil {
		logger.ErrorC(ctx, "Error during context serializing: %+v", err)
		return req, err
	}

	return req, nil
}

func SecureWebSocketDial(logContext context.Context, webSocketURL url.URL, dialer websocket.Dialer, requestHeaders http.Header, logger logging.Logger) (*websocket.Conn, *http.Response, error) {
	m2mToken, err := getConfig().getToken(logContext)
	if err != nil {
		logger.ErrorC(logContext, "Can't refresh token %v", err)
		return nil, nil, err
	}
	if requestHeaders == nil {
		logger.WarnC(logContext, "Headers are nil. Creating default headers")
		requestHeaders = http.Header{}
	}
	requestHeaders = addHeaderIfAbsent(requestHeaders, "Host", webSocketURL.Host)
	requestHeaders = addHeaderIfAbsent(requestHeaders, "Authorization", "Bearer "+m2mToken)
	return dialer.Dial(webSocketURL.String(), requestHeaders)
}

func addHeaderIfAbsent(requestHeaders http.Header, headerName, headerValue string) http.Header {
	if _, ok := requestHeaders[headerName]; !ok {
		requestHeaders.Add(headerName, headerValue)
	}
	return requestHeaders
}
