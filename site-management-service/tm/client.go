package tm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-errors/errors"
	pgdbaas "github.com/netcracker/qubership-core-lib-go-dbaas-postgres-client/v4"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/dao/pg"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/utils"
	"github.com/valyala/fasthttp"
	"sync"
	"time"
)

var tenantManagerPath = "http://internal-gateway-service:8080/api/v4/tenant-manager"

const (
	tenantsApiPath  = "/manage/tenants"
	StatusActive    = "ACTIVE"
	StatusSuspended = "SUSPENDED"
)

type Client struct {
	sync.RWMutex
	wg                 sync.WaitGroup
	dao                *pg.RouteManagerDao
	wsConnector        WebSocketConnector
	watchingActive     bool
	wsConnected        bool
	quit               chan int
	callbacksMap       map[TenantWatchEventType][]func(ctx context.Context, event TenantWatchEvent) error
	wsRetryTimeout     time.Duration
	activeTenantsCache sync.Map
}

type Admin struct {
	Email string `json:"login"`
}

type Tenant struct {
	ObjectId    string `json:"objectId"`
	ExternalId  string `json:"externalId"`
	DomainName  string `json:"domainName"`
	ServiceName string `json:"serviceName"`
	TenantName  string `json:"name"`
	Status      string `json:"status"`
	Namespace   string `json:"namespace"`
	User        Admin  `json:"admin"`
}

var (
	logger             logging.Logger
	ErrTenantNotFound  = errors.New("tenant not found in tenant-manager")
	doRetryRequestFunc = utils.DoRetryRequest
)

func init() {
	logger = logging.GetLogger("tenantmanagerclient")
}

func NewClient(pgClient pgdbaas.PgClient, wsConnector WebSocketConnector, wsRetryTimeout time.Duration) *Client {
	client := Client{
		dao:            pg.NewRouteManager(pgClient),
		wsConnector:    wsConnector,
		quit:           make(chan int),
		callbacksMap:   make(map[TenantWatchEventType][]func(context.Context, TenantWatchEvent) error),
		wg:             sync.WaitGroup{},
		wsRetryTimeout: wsRetryTimeout,
	}
	return &client
}

func (c *Client) GetTenantByExternalId(ctx context.Context, externalId string) (*Tenant, error) {
	logger.InfoC(ctx, "Get tenant from tenant-manager by external id: '%s'", externalId)
	return c.getTenantById(ctx, externalId, "externalId")
}

func (c *Client) GetTenantByObjectId(ctx context.Context, objectId string) (*Tenant, error) {
	logger.InfoC(ctx, "Get tenant from tenant-manager by object id: '%s'", objectId)
	return c.getTenantById(ctx, objectId, "objectId")
}

func (c *Client) getTenantById(ctx context.Context, id string, idName string) (*Tenant, error) {
	logger.InfoC(ctx, "Get tenant from tenant-manager by %s: '%s'", idName, id)
	url := tenantManagerPath + tenantsApiPath + "/" + id
	resp, err := performRequest(ctx, url, fasthttp.MethodGet, nil)
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while getting tenant '%s' from tenant-manager: %s", id, err.Error())
		return nil, err
	}
	defer fasthttp.ReleaseResponse(resp)

	var tenant Tenant
	if resp.StatusCode() == 404 {
		logger.WarnC(ctx, "Tenant with id %s not found in tenant-manager. It will be deleted from site-management database", id)
		if err := c.dao.Delete(ctx, id); err != nil {
			logger.ErrorC(ctx, "Error occurred while deleting missing tenant from site-management: %s", err.Error())
		}
		return nil, ErrTenantNotFound
	} else {
		decoder := json.NewDecoder(bytes.NewReader(resp.Body()))
		if err := decoder.Decode(&tenant); err != nil {
			logger.ErrorC(ctx, "Error occurred while marshalling response body into tenant: %s", err.Error())
			return nil, err
		}
	}

	return &tenant, nil
}

func (c *Client) GetAllTenantsByStatus(ctx context.Context, status string) (*[]Tenant, error) {
	logger.InfoC(ctx, "Get all tenant from tenant-manager with status %s", status)
	endpoint := tenantManagerPath + tenantsApiPath
	if status != "" {
		endpoint += "?search=status=" + status
	}
	resp, err := performRequest(ctx, endpoint, fasthttp.MethodGet, nil)
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while getting all tenants by status'%s' from tenant-manager: %s", status, err.Error())
		return nil, err
	}
	defer fasthttp.ReleaseResponse(resp)
	tenants := []Tenant{}
	decoder := json.NewDecoder(bytes.NewReader(resp.Body()))
	if err := decoder.Decode(&tenants); err != nil {
		logger.ErrorC(ctx, "Error occurred while marshalling response body into tenant: %s", err.Error())
		return nil, err
	}
	return &tenants, nil
}

func (c *Client) UpdateActiveTenantsCache(ctx context.Context, tenants []Tenant) {
	logger.DebugC(ctx, "Received %d tenants to update activeTenantCache", len(tenants))
	for _, tenant := range tenants {
		if tenant.Status == StatusActive {
			logger.DebugC(ctx, "storing %v, value: %v", tenant.ExternalId, tenant)
			c.activeTenantsCache.Store(tenant.ExternalId, tenant)
		}
		if tenant.Status == StatusSuspended {
			c.activeTenantsCache.Delete(tenant.ExternalId)
		}
	}
}

func (c *Client) DeleteFromActiveTenantsCache(ctx context.Context, tenants []Tenant) {
	logger.DebugC(ctx, "Received %d tenants to update activeTenantCache", len(tenants))
	for _, tenant := range tenants {
		c.activeTenantsCache.Delete(tenant.ExternalId)
	}
}

func (c *Client) GetActiveTenantsCache(ctx context.Context) []Tenant {
	var resultTenants []Tenant
	c.activeTenantsCache.Range(func(key, value interface{}) bool {
		resultTenants = append(resultTenants, value.(Tenant))
		return true
	})
	logger.DebugC(ctx, "Found %d active tenants in cache", len(resultTenants))
	return resultTenants
}

func performRequest(ctx context.Context, url, method string, body []byte) (*fasthttp.Response, error) {
	logger.InfoC(ctx, "Perform request with URL '%s', method '%s'", url, method)
	resp, err := doRetryRequestFunc(ctx, method, url, body, logger)
	if err != nil {
		logger.ErrorC(ctx, "Error while making new request with url %s and method %s", url, method)
		return nil, err
	}
	if resp.StatusCode() >= 400 {
		logger.WarnC(ctx, "Response status code: %v", resp.StatusCode())
	} else {
		logger.InfoC(ctx, "Got %d status code in response", resp.StatusCode())
	}
	return resp, nil
}

func (t *Tenant) String() string {
	return fmt.Sprintf("Tenant{objectId=%s,externalId=%s,domainName=%s,serviceName=%s,tenantName=%s,status=%s,namespace=%s,user=%s}",
		t.ObjectId, t.ExternalId, t.DomainName, t.ServiceName, t.TenantName, t.Status, t.Namespace, t.User)
}

func (a Admin) String() string {
	return fmt.Sprintf("Admin{email=%s}", a.Email)
}
