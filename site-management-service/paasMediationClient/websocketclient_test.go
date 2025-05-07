package paasMediationClient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/paasMediationClient/domain"
	. "github.com/smarty/assertions"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

type fakeWebsocketExecutor struct {
	handler http.Handler
	err     error
}

type badFakeWebsocketExecutor struct {
	attemptCounter int
	handler        http.Handler
	err            error
}

func (*fakeWebsocketExecutor) collectHeaders(ctx context.Context, idpAddress url.URL) (http.Header, error) {
	return http.Header{}, nil
}

func (executor *badFakeWebsocketExecutor) collectHeaders(ctx context.Context, idpAddress url.URL) (http.Header, error) {
	return http.Header{}, nil
}

func (fakeWebsocketExecutor *fakeWebsocketExecutor) createWebsocketConnect(targetAddress url.URL, header http.Header) (*websocket.Conn, *http.Response, error) {
	server := httptest.NewServer(fakeWebsocketExecutor.handler)
	defer server.Close()
	dialer := websocket.Dialer{}
	address := server.Listener.Addr().String
	logger.Info("Address %s", address())
	return dialer.Dial("ws://"+address(), nil)
}

func (executor *badFakeWebsocketExecutor) createWebsocketConnect(targetAddress url.URL, header http.Header) (*websocket.Conn, *http.Response, error) {
	if executor.attemptCounter >= 10 {
		panic("Panic during websocket connect creation due to no more attempts")
	} else {
		executor.attemptCounter++
	}
	return nil, nil, errors.New("Fail websocket connect creation for test")
}

func TestCreateWebSocketClientForRoute(t *testing.T) {
	fakeExecutor := fakeWebsocketExecutor{}
	webSocketClient := WebSocketClient{
		websocketExecutor: &fakeExecutor,
		resource:          routesString,
		namespace:         "test-namespace",
		bus:               make(chan []byte, 50),
	}
	fakeExecutor.handler = http.HandlerFunc(createRouteHandler)
	go webSocketClient.initWebsocketClient(context.Background(), url.URL{Scheme: "ws", Host: "localhost:8080", Path: webSocketClient.generatePath()})
	timer := time.After(3 * time.Second)
	select {
	case <-timer:
		panic("Event on route creation was not gotten")
	case result := <-webSocketClient.bus:
		logger.Info("result %s", result)
		var routeUpdate = new(RouteUpdate)
		if err := json.Unmarshal(result, routeUpdate); err != nil {
			panic(err)
		}
		assertResult(So(routeUpdate.Type, ShouldEqual, updateTypeInit))

		result = <-webSocketClient.bus
		logger.Info("result %s", result)
		routeUpdate = new(RouteUpdate)
		if err := json.Unmarshal(result, routeUpdate); err != nil {
			panic(err)
		}

		assertResult(So(routeUpdate.Type, ShouldEqual, updateTypeAdded))
		assertResult(So(routeUpdate.RouteObject, ShouldResemble, domain.Route{Metadata: domain.Metadata{Name: "test-route", Namespace: "test-namespace"}}))
	}
}

func TestCreateWebSocketClientInitSignal(t *testing.T) {
	fakeExecutor := fakeWebsocketExecutor{}
	webSocketClient := WebSocketClient{
		websocketExecutor: &fakeExecutor,
		resource:          routesString,
		namespace:         "test-namespace",
		bus:               make(chan []byte, 50),
	}
	fakeExecutor.handler = http.HandlerFunc(closeOpenedConnection)
	go webSocketClient.initWebsocketClient(context.Background(), url.URL{Scheme: "ws", Host: "localhost:8080", Path: webSocketClient.generatePath()})
	timer := time.After(3 * time.Second)
	select {
	case <-timer:
		panic("Init signal has not been received")
	case result := <-webSocketClient.bus:
		logger.Info("result %s", result)
		var commonUpdate = new(CommonUpdate)
		if err := json.Unmarshal(result, commonUpdate); err != nil {
			panic(err)
		}
		assertResult(So(commonUpdate.Type, ShouldEqual, updateTypeInit))
		assertResult(So(commonUpdate.CommonObject, ShouldResemble, domain.CommonObject{Metadata: domain.Metadata{Namespace: "test-namespace"}}))
	}
}

func TestReconnectWebSocketClient(t *testing.T) {
	fakeExecutor := badFakeWebsocketExecutor{}
	webSocketClient := WebSocketClient{
		websocketExecutor: &fakeExecutor,
		resource:          routesString,
		namespace:         "test-namespace",
		bus:               make(chan []byte, 50),
	}
	fakeExecutor.handler = http.HandlerFunc(closeOpenedConnection)
	assert.Panics(t, func() {
		webSocketClient.initWebsocketClient(context.Background(), url.URL{Scheme: "ws", Host: "localhost:8080", Path: webSocketClient.generatePath()})
	})
}

func createRouteHandler(w http.ResponseWriter, r *http.Request) {
	logger.Info("Get request on websocket connect")
	upgrader := websocket.Upgrader{}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()
	routeUpdate := RouteUpdate{
		Type:        updateTypeAdded,
		RouteObject: domain.Route{Metadata: domain.Metadata{Name: "test-route", Namespace: "test-namespace"}},
	}
	reqBodyBytes := new(bytes.Buffer)
	if err := json.NewEncoder(reqBodyBytes).Encode(routeUpdate); err != nil {
		panic(err)
	}
	reqBodyBytes.Bytes()
	err = c.WriteMessage(websocket.TextMessage, reqBodyBytes.Bytes())
}

func closeOpenedConnection(w http.ResponseWriter, r *http.Request) {
	logger.Info("Get request on websocket connect")
	upgrader := websocket.Upgrader{}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c.Close()
}
