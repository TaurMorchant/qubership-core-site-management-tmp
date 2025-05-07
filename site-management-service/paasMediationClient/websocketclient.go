package paasMediationClient

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-lib-go/v3/security"
	"github.com/netcracker/qubership-core-lib-go/v3/serviceloader"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/paasMediationClient/domain"
	"io/ioutil"
	"net/http"
	"net/url"
)

var loggerWS logging.Logger

type (
	WebSocketClient struct {
		bus               chan []byte
		namespace         string
		resource          string
		websocketExecutor websocketExecutor
	}
	websocketExecutor interface {
		collectHeaders(ctx context.Context, idpAddress url.URL) (http.Header, error)
		createWebsocketConnect(targetAddress url.URL, header http.Header) (*websocket.Conn, *http.Response, error)
	}
	defaultWebsocketExecutor struct {
	}
)

func init() {
	loggerWS = logging.GetLogger("webSocketClient")
}

func CreateWebSocketClient(ctx context.Context, channel *chan []byte, host, namespace, resource string) {
	loggerWS.InfoC(ctx, "Create new web socket client for host '%s', namespace '%s', resource '%s'", host, namespace, resource)
	client := &WebSocketClient{
		bus:               *channel,
		namespace:         namespace,
		resource:          resource,
		websocketExecutor: &defaultWebsocketExecutor{},
	}
	u := url.URL{Scheme: "ws", Host: host, Path: client.generatePath()}

	go client.initWebsocketClient(ctx, u)
}

func (c *WebSocketClient) String() string {
	return fmt.Sprintf("WebSocketClient{namespace=%s,resource=%s,websocketExecutor=%s}", c.namespace, c.resource, c.websocketExecutor)
}

func (c *WebSocketClient) generatePath() string {
	return fmt.Sprintf("/watchapi/v2/paas-mediation/namespaces/%s/%s", c.namespace, c.resource)
}

func (c *WebSocketClient) initWebsocketClient(ctx context.Context, u url.URL) {
	for {
		loggerWS.InfoC(ctx, "Initialize web socket client with address: '%s'", u.String())

		header, err := c.websocketExecutor.collectHeaders(ctx, u)
		if err != nil {
			loggerWS.ErrorC(ctx, "Error during headers collections: %+v", err.Error())
		}

		conn, resp, err := c.websocketExecutor.createWebsocketConnect(u, header)
		if err != nil {
			if resp != nil {
				b, _ := ioutil.ReadAll(resp.Body)
				loggerWS.ErrorC(ctx, resp.Status)
				loggerWS.ErrorC(ctx, string(b))
			}
			loggerWS.ErrorC(ctx, "dial error with url '%s': %s", u.String(), err)
			continue
		}
		loggerWS.InfoC(ctx, "Status: %s", resp.Status)

		//init signal to get snapshot of resources after connect to websocket
		initSignal, errM := json.Marshal(
			CommonUpdate{
				Type: updateTypeInit,
				CommonObject: domain.CommonObject{
					Metadata: domain.Metadata{Namespace: c.namespace},
				},
			})
		if errM != nil {
			loggerWS.ErrorC(ctx, "Error occurred while marshalling init signal: %s", err.Error())
		} else {
			loggerWS.InfoC(ctx, "Init cache signal: %s", u.String())
			c.bus <- initSignal
		}
		for {
			_, message, err := conn.ReadMessage()
			logger.DebugC(ctx, "Received message by websocket: %s", message)
			if err != nil {
				loggerWS.ErrorC(ctx, "read error from url '%s': %s\nTry to establish web socket connection", u.String(), err)
				conn.Close()
				break
			} else {
				c.bus <- message
			}
		}
	}
}

func (*defaultWebsocketExecutor) createWebsocketConnect(targetAddress url.URL, header http.Header) (*websocket.Conn, *http.Response, error) {
	dialer := websocket.Dialer{}
	return dialer.Dial(targetAddress.Scheme+"://"+targetAddress.Host+targetAddress.Path, header)
}

func (websocketExecutor *defaultWebsocketExecutor) collectHeaders(ctx context.Context, url url.URL) (http.Header, error) {
	m2mTokenProvider := serviceloader.MustLoad[security.TokenProvider]()
	m2mToken, err := m2mTokenProvider.GetToken(ctx)
	if err != nil {
		loggerWS.ErrorC(ctx, "Error during acquiring m2m token: %+v", err)
		return nil, err
	}
	header := http.Header{}
	header.Add("Authorization", fmt.Sprintf("Bearer %s", m2mToken))
	header.Add("Content-Type", "application/json")
	header.Add("Host", url.Host)
	header.Add("Origin", "https://"+url.Host)
	return header, nil
}
