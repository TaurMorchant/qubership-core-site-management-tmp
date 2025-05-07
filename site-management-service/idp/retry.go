package idp

import (
	"context"
	"github.com/go-errors/errors"
	"github.com/mitchellh/hashstructure/v2"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"time"
)

var log = logging.GetLogger("idp")

type ClientWithRetry struct {
	cancel context.CancelFunc
	hash   uint64
	ch     chan URIRequest
	quit   chan int
	client Client
}

func NewClientWithRetry(client Client) *ClientWithRetry {
	retryClient := &ClientWithRetry{
		hash:   0,
		ch:     make(chan URIRequest, 10),
		quit:   make(chan int, 1),
		client: client,
	}

	return retryClient
}

func (c *ClientWithRetry) CheckPostURIFeature(ctx context.Context) (bool, error) {
	for { // infinite retries will prevent service to start without knowing is PostURI feature supported or not
		if endpointExists, err := c.client.CheckPostURIFeature(ctx); err != nil {
			log.ErrorC(ctx, "Failed to send request to IDP %s. Trying again...", err)
			time.Sleep(5 * time.Second)
			continue
		} else {
			return endpointExists, nil
		}
	}
}

func (c *ClientWithRetry) PostURI(ctx context.Context, request URIRequest) error {
	hash, err := hashstructure.Hash(request, hashstructure.FormatV2, &hashstructure.HashOptions{SlicesAsSets: true})
	if err != nil {
		return errors.Wrap(err, 0)
	}
	if hash != c.hash {
		c.hash = hash
		if c.cancel != nil {
			c.cancel()
		}
		var cancelCtx context.Context
		cancelCtx, c.cancel = context.WithCancel(ctx)
		go func(ctx context.Context, request URIRequest) {
			err := c.client.PostURI(ctx, request)
			if err != nil {
				log.ErrorC(ctx, "Failed to send request to IDP %s. Trying again...", err)
				for {
					select {
					case <-ctx.Done():
						return
					case <-time.After(5 * time.Second):
						err := c.client.PostURI(ctx, request)
						if err == nil {
							return
						}
						log.ErrorC(ctx, "Failed to send request to IDP %s. Trying again...", err)
					}
				}
			}
		}(cancelCtx, request)
	} else {
		log.WarnC(ctx, "Request with the same data was already send. Skipping...")
	}
	return nil
}

func (c *ClientWithRetry) Reset() {
	c.hash = 0
}
