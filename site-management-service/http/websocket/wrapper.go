package websocket

import (
	"context"
	"github.com/go-errors/errors"
	"github.com/gorilla/websocket"
	stompws "github.com/netcracker/qubership-core-lib-go-stomp-websocket/v3"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"net/http"
	"net/url"
)

var logger logging.Logger

func init() {
	logger = logging.GetLogger("websocket")
}

type ConnectFunc func(url.URL, websocket.Dialer, http.Header, stompws.ConnectionDialer) (*stompws.StompClient, error)

type Connector struct {
	dialer           websocket.Dialer
	header           http.Header
	connectionDialer stompws.ConnectionDialer
	stompClient      *stompws.StompClient
	connect          ConnectFunc
}

func newConnector(connectFunc ConnectFunc) *Connector {
	connector := &Connector{
		dialer:           websocket.Dialer{},
		header:           http.Header{},
		connectionDialer: &ConnectionDial{},
		connect:          connectFunc,
	}
	return connector
}

func NewConnector() *Connector {
	return newConnector(stompws.Connect)
}

func (c *Connector) ConnectAndSubscribe(ctx context.Context, destination string, topic string) (*stompws.Subscription, error) {
	wsURL, err := url.Parse(destination)
	if err != nil {
		return nil, errors.New(err)
	}
	delete(c.header, "Authorization")
	stompClient, err := c.connect(*wsURL, c.dialer, c.header, c.connectionDialer)
	if err != nil {
		logger.ErrorC(ctx, "Can't connect to websocket: %s", err)
		return nil, errors.New(err)
	}
	subscription, err := stompClient.Subscribe(topic)
	if err != nil {
		logger.ErrorC(ctx, "Can't subscribe to topic: %s", err)
		return nil, errors.New(err)
	}
	logger.InfoC(ctx, "Subscribed to '%s' topic", topic)
	return subscription, nil
}

func (c *Connector) Disconnect(ctx context.Context) error {
	if c.stompClient != nil {
		logger.InfoC(ctx, "Closing websocket connection")
		return c.stompClient.Disconnect()
	}
	return nil
}
