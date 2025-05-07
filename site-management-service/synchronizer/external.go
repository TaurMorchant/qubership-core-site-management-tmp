package synchronizer

import (
	"context"
)

type IDPFacade interface {
	CheckPostURIFeature(ctx context.Context) (bool, error)
	SetRedirectURIs(ctx context.Context, tenantURIs map[string][]string, commonURIs []string) error
}
