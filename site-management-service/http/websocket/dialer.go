package websocket

import (
	"context"
	"github.com/gorilla/websocket"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/utils"
	"net/http"
	"net/url"
)

type ConnectionDial struct {
}

func (dial ConnectionDial) Dial(webSocketURL url.URL, dialer websocket.Dialer, requestHeaders http.Header) (*websocket.Conn, *http.Response, error) {
	ctx := context.Background()
	return utils.SecureWebSocketDial(ctx, webSocketURL, dialer, requestHeaders, logger)
}
