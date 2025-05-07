package tm_test

import (
	"context"
	"encoding/json"
	"github.com/go-errors/errors"
	dbaasbase "github.com/netcracker/qubership-core-lib-go-dbaas-base-client/v3"
	pgdbaas "github.com/netcracker/qubership-core-lib-go-dbaas-postgres-client/v4"
	stompws "github.com/netcracker/qubership-core-lib-go-stomp-websocket/v3"
	"github.com/netcracker/qubership-core-lib-go/v3/configloader"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/tm"
	"github.com/stretchr/testify/assert"
	"os"
	"sync"
	"testing"
	"time"
)

type wsConnectorMock struct {
	ConnectAndSubMock func(context.Context, string, string) (*stompws.Subscription, error)
	DisconnectMock    func(context.Context) error
}

func (w *wsConnectorMock) ConnectAndSubscribe(ctx context.Context, webSocketURL string, topic string) (*stompws.Subscription, error) {
	return w.ConnectAndSubMock(ctx, webSocketURL, topic)
}

func (w *wsConnectorMock) Disconnect(ctx context.Context) error {
	return w.DisconnectMock(ctx)
}

func TestClient_WatcherMainCases(t *testing.T) {
	timeout := time.After(5 * time.Second)
	done := make(chan int)
	os.Setenv("microservice.namespace", "test")
	os.Setenv("microservice.name", "test")
	configloader.Init(configloader.EnvPropertySource())
	defer func() {
		os.Unsetenv("microservice.namespace")
		os.Unsetenv("microservice.name")
	}()

	go func() {
		connectAndSubscribeCallTimes := 0
		disconnectCallTimes := 0
		frameCh := make(chan *stompws.Frame)
		wsConnector := &wsConnectorMock{
			ConnectAndSubMock: func(ctx context.Context, websocketURL string, topic string) (*stompws.Subscription, error) {
				connectAndSubscribeCallTimes++
				return &stompws.Subscription{
					FrameCh: frameCh,
					Id:      "",
					Topic:   "",
				}, nil
			},
			DisconnectMock: func(ctx context.Context) error {
				disconnectCallTimes++
				return nil
			},
		}
		ctx := context.Background()
		dbaasPool := dbaasbase.NewDbaaSPool()
		client, err := pgdbaas.NewClient(dbaasPool).ServiceDatabase().GetPgClient()
		assert.Nil(t, err)
		tmClient := tm.NewClient(client, wsConnector, 1*time.Second)
		fireEventCallTimes := 0
		wg := sync.WaitGroup{}
		wg.Add(3)
		tmClient.SubscribeToAllExcept(tm.EventTypeDeleted, func(ctx context.Context, event tm.TenantWatchEvent) error {
			assert.NotEqual(t, tm.EventTypeDeleted, event.Type)
			fireEventCallTimes++
			wg.Done()
			return nil
		})
		tmClient.StartWatching(ctx)

		// We send wrong frames and expect what watcher doesn't stop working
		// If watcher stop working next send to channel will stuck and test fail with timeout
		frameCh <- &stompws.Frame{Body: ""}                           // body with zero length
		frameCh <- &stompws.Frame{Body: "{\"-d"}                      // wrong json
		frameCh <- &stompws.Frame{Body: "{\"type\": \"SUBSCRIBED\"}"} // no tenants
		frameCh <- &stompws.Frame{Body: createTenantEventJSON(tm.EventTypeCreated)}
		frameCh <- &stompws.Frame{Body: createTenantEventJSON(tm.EventTypeSubscribed)}
		frameCh <- &stompws.Frame{Body: createTenantEventJSON(tm.EventTypeModified)}
		frameCh <- &stompws.Frame{Body: createTenantEventJSON(tm.EventTypeDeleted)}

		wg.Wait()
		tmClient.StopWatching(ctx)
		assert.Equal(t, 1, connectAndSubscribeCallTimes)
		assert.Equal(t, 1, disconnectCallTimes)
		assert.Equal(t, 3, fireEventCallTimes)
		done <- 0
	}()

	select {
	case <-timeout:
		t.Fatalf("Test didn't finish in time")
	case <-done:
	}
}

func TestClient_PanicCallback(t *testing.T) {
	timeout := time.After(5 * time.Second)
	done := make(chan int)

	go func() {
		connectAndSubscribeCallTimes := 0
		disconnectCallTimes := 0
		frameCh := make(chan *stompws.Frame)
		wsConnector := &wsConnectorMock{
			ConnectAndSubMock: func(ctx context.Context, websocketURL string, topic string) (*stompws.Subscription, error) {
				connectAndSubscribeCallTimes++
				return &stompws.Subscription{
					FrameCh: frameCh,
					Id:      "",
					Topic:   "",
				}, nil
			},
			DisconnectMock: func(ctx context.Context) error {
				disconnectCallTimes++
				return nil
			},
		}
		ctx := context.Background()
		dbaasPool := dbaasbase.NewDbaaSPool()
		client, err := pgdbaas.NewClient(dbaasPool).ServiceDatabase().GetPgClient()
		assert.Nil(t, err)
		tmClient := tm.NewClient(client, wsConnector, 1*time.Second)
		wg := sync.WaitGroup{}
		wg.Add(1)
		tmClient.SubscribeToEvent(tm.EventTypeDeleted, func(ctx context.Context, event tm.TenantWatchEvent) error {
			wg.Done()
			panic("wops!")
			return nil
		})
		tmClient.StartWatching(ctx)

		frameCh <- &stompws.Frame{Body: createTenantEventJSON(tm.EventTypeDeleted)}

		wg.Wait()
		tmClient.StopWatching(ctx)
		assert.Equal(t, 1, connectAndSubscribeCallTimes)
		assert.Equal(t, 1, disconnectCallTimes)
		done <- 0
	}()

	select {
	case <-timeout:
		t.Fatalf("Test didn't finish in time")
	case <-done:
	}
}

func TestClient_WebSocketClosed(t *testing.T) {
	timeout := time.After(5 * time.Second)
	done := make(chan int)

	go func() {
		connectAndSubscribeCallTimes := 0
		disconnectCallTimes := 0
		frameCh := make(chan *stompws.Frame)
		wsConnector := &wsConnectorMock{
			ConnectAndSubMock: func(ctx context.Context, websocketURL string, topic string) (*stompws.Subscription, error) {
				connectAndSubscribeCallTimes++
				return &stompws.Subscription{
					FrameCh: frameCh,
					Id:      "",
					Topic:   "",
				}, nil
			},
			DisconnectMock: func(ctx context.Context) error {
				disconnectCallTimes++
				return nil
			},
		}
		ctx := context.Background()
		dbaasPool := dbaasbase.NewDbaaSPool()
		client, err := pgdbaas.NewClient(dbaasPool).ServiceDatabase().GetPgClient()
		assert.Nil(t, err)
		tmClient := tm.NewClient(client, wsConnector, 1*time.Second)
		wg := sync.WaitGroup{}
		wg.Add(1)
		fireEventCallTimes := 0
		tmClient.SubscribeToEvent(tm.EventTypeDeleted, func(ctx context.Context, event tm.TenantWatchEvent) error {
			fireEventCallTimes++
			wg.Done()
			return nil
		})
		tmClient.StartWatching(ctx)
		wg.Add(1)
		frameCh <- &stompws.Frame{Body: createTenantEventJSON(tm.EventTypeDeleted)}
		wsConnector.ConnectAndSubMock = func(ctx context.Context, s string, s2 string) (*stompws.Subscription, error) {
			connectAndSubscribeCallTimes++
			wg.Done()
			return nil, errors.New("error")
		}
		close(frameCh)

		wg.Wait()
		tmClient.StopWatching(ctx)
		assert.Equal(t, 2, connectAndSubscribeCallTimes)
		assert.Equal(t, 1, disconnectCallTimes)
		assert.Equal(t, 1, fireEventCallTimes)
		done <- 0
	}()

	select {
	case <-timeout:
		t.Fatalf("Test didn't finish in time")
	case <-done:
	}
}

func createTenantEventJSON(eventType tm.TenantWatchEventType) string {
	event := &tm.TenantWatchEvent{
		Type: eventType,
		Tenants: []tm.Tenant{
			{ObjectId: ""},
		},
	}
	encodedEvent, err := json.Marshal(event)
	if err != nil {
		panic(err)
	}
	return string(encodedEvent)
}
