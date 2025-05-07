package tm

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-errors/errors"
	stompws "github.com/netcracker/qubership-core-lib-go-stomp-websocket/v3"
	"runtime/debug"
	"time"
)

type TenantWatchEventType string

var webSocketPath = "ws://internal-gateway-service:8080/api/v4/tenant-manager/watch"

const (
	tenantTopic = "/channels/tenants"
)

const (
	EventTypeSubscribed TenantWatchEventType = "SUBSCRIBED"
	EventTypeCreated    TenantWatchEventType = "CREATED"
	EventTypeModified   TenantWatchEventType = "MODIFIED"
	EventTypeDeleted    TenantWatchEventType = "DELETED"
)

var eventTypes = []TenantWatchEventType{
	EventTypeSubscribed,
	EventTypeCreated,
	EventTypeModified,
	EventTypeDeleted,
}

type TenantWatchEvent struct {
	Type    TenantWatchEventType `json:"type"`
	Tenants []Tenant             `json:"tenants,omitempty"`
}

func (e *TenantWatchEvent) String() string {
	return fmt.Sprintf("TenantWatchEvent{type=%s,tenants=%s}", e.Type, e.Tenants)
}

type WebSocketConnector interface {
	ConnectAndSubscribe(ctx context.Context, webSocketURL string, topic string) (*stompws.Subscription, error)
	Disconnect(ctx context.Context) error
}

func (c *Client) SubscribeToEvent(event TenantWatchEventType, callback func(context.Context, TenantWatchEvent) error) {
	c.Lock()
	defer c.Unlock()
	c.callbacksMap[event] = append(c.callbacksMap[event], callback)
}

func (c *Client) SubscribeToAll(callback func(ctx context.Context, event TenantWatchEvent) error) {
	c.Lock()
	defer c.Unlock()
	for _, eventType := range eventTypes {
		c.callbacksMap[eventType] = append(c.callbacksMap[eventType], callback)
	}
}

func (c *Client) SubscribeToAllExcept(subEventType TenantWatchEventType, callback func(context.Context, TenantWatchEvent) error) {
	c.Lock()
	defer c.Unlock()
	for _, eventType := range eventTypes {
		if eventType != subEventType {
			c.callbacksMap[eventType] = append(c.callbacksMap[eventType], callback)
		}
	}
}

// StartWatching method runs goroutine with two cycles. First cycle connects to tenant-manager web socket.
// If connection failed it would try one more time after one second. Reading frames from tenant-manager also.
// To stop goroutine use method StopWatching
// ctx context.Context argument uses only for logging
func (c *Client) StartWatching(ctx context.Context) {
	c.watchingActive = true
	logger.InfoC(ctx, "Watching tenant-manager has been started.")
	go func() {
		c.wg.Add(1)
		defer c.wg.Done()
		for c.watchingActive {
			subscription, err := c.wsConnector.ConnectAndSubscribe(ctx, webSocketPath, tenantTopic)
			if err != nil {
				logger.ErrorC(ctx, "Failed to prepare websocket connection %s", err)
				time.Sleep(c.wsRetryTimeout)
				continue
			}
			c.wsConnected = true
			func() {
				defer func() {
					if err := c.wsConnector.Disconnect(ctx); err != nil {
						logger.ErrorC(ctx, "Failed to disconnect websocket: %s", err)
					}
					c.wsConnected = false
				}()
				for {
					select {
					case frame, ok := <-subscription.FrameCh:
						if !ok {
							logger.WarnC(ctx, "Can't read from websocket. Connection was closed by server")
							time.Sleep(c.wsRetryTimeout)
							return
						}
						logger.DebugC(ctx, "Received frame from tenant-manager %s", frame)
						var tenantEvent = new(TenantWatchEvent)
						if len(frame.Body) == 0 {
							logger.WarnC(ctx, "Received frame from tenant-manager has empty Body.")
							continue
						}
						if err := json.Unmarshal([]byte(frame.Body), tenantEvent); err != nil {
							logger.ErrorC(ctx, "Failed to unmarshal frame body. Frame body: '%s'. Error: %s", frame.Body, err)
							continue
						}
						logger.InfoC(ctx, "Received tenantWatchEvent of type '%s'", tenantEvent.Type)
						if tenantEvent.Tenants == nil {
							logger.WarnC(ctx, "Received frame has no tenants. Event: '%s'", tenantEvent)
							continue
						}
						c.RLock()
						if callbacks, ok := c.callbacksMap[tenantEvent.Type]; ok {
							for _, callback := range callbacks {
								func() {
									defer func() {
										if r := recover(); r != nil {
											logger.ErrorC(ctx, "Callback threw panic: \n%s", debug.Stack())
										}
									}()
									if err := callback(ctx, *tenantEvent); err != nil {
										logger.ErrorC(ctx, "One of callbacks failed with error: %s.", err)
										if stackErr, ok := err.(*errors.Error); ok {
											logger.ErrorC(ctx, "Stack: %s", stackErr.ErrorStack())
										}
									}
								}()
							}
						} else {
							logger.WarnC(ctx, "There are no registered callbacks for event type %s", tenantEvent.Type)
						}
						c.RUnlock()
					case <-c.quit:
						return
					}
				}
			}()
		}
		logger.InfoC(ctx, "Watching tenant-manager has been stopped.")
	}()
}

// StopWatching stops watching gracefully
func (c *Client) StopWatching(ctx context.Context) {
	c.watchingActive = false
	logger.InfoC(ctx, "Watching tenant-manager is shutting down...")
	if c.wsConnected {
		c.quit <- 0
		c.wg.Wait()
	}
}
