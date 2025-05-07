package paasMediationClient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-errors/errors"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	wrappers "github.com/netcracker/qubership-core-site-management/site-management-service/v2/domain/wrappers"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/paasMediationClient/domain"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/utils"
	"github.com/valyala/fasthttp"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

type (
	RoutesCache struct {
		routes    map[string]*map[string]domain.Route
		mutex     *sync.RWMutex
		bus       chan []byte
		initCache func(context.Context, string)
	}
	ServicesCache struct {
		services  map[string]*map[string]domain.Service
		mutex     *sync.RWMutex
		bus       chan []byte
		initCache func(context.Context, string)
	}

	ConfigMapsCache struct {
		configMaps map[string]*map[string]domain.Configmap
		mutex      *sync.RWMutex
		bus        chan []byte
		initCache  func(context.Context, string)
	}

	CompositeCache struct {
		routesCache         *RoutesCache
		servicesCache       *ServicesCache
		configMapsCache     *ConfigMapsCache
		lastCacheUpdateTime time.Time
	}

	PaasMediationClient struct {
		InternalGatewayAddress *url.URL
		Namespace              string
		cache                  *CompositeCache
		httpExecutor           httpExecutor
		callbacks              []RoutesCallback
	}

	httpExecutor interface {
		doRequest(ctx context.Context, url, method string, body []byte) (*fasthttp.Response, error)
	}

	httpExecutorImpl struct {
	}

	CommonUpdateStr struct {
		updateCache func(context.Context, interface{})
		resource    interface{}
		bus         chan []byte
		lastUpdate  chan time.Time
	}
)

type RoutesCallback func(context.Context, *PaasMediationClient, RouteUpdate) error

const (
	routesString             string = "routes"
	configmapsString         string = "configmaps"
	servicesString           string = "services"
	ProjectTypeConfigMapName string = "baseline-version"
	TMConfigsConfigmapName   string = "tenant-manager-configs"
	attempts                 int    = 12
	attemptsWs               int    = 10
	attemptsInit             int    = 5
	period                          = 5 * time.Second
	sleepWs                         = 2 * time.Minute
	sleepInit                       = 1 * time.Second
)

var logger logging.Logger

func init() {
	logger = logging.GetLogger("paasMediationClient")
}

func NewClient(ctx context.Context, internalGatewayAddress *url.URL, namespace string) *PaasMediationClient {
	if internalGatewayAddress == nil {
		panic(fmt.Sprintf("Parameters \"internalGatewayAddress\" and \"idpAddress\" can not be empty!"))
	}
	client := &PaasMediationClient{InternalGatewayAddress: internalGatewayAddress, Namespace: namespace}
	client.httpExecutor = createDefaultHttpExecutor()
	client.initCompositeCache(ctx)
	return client
}

func createDefaultHttpExecutor() *httpExecutorImpl {
	return &httpExecutorImpl{}
}

func (c *PaasMediationClient) AddRouteCallback(callback RoutesCallback) {
	c.callbacks = append(c.callbacks, callback)
}

func (cache *CompositeCache) updateRoutesCache(ctx context.Context, routeUpdateI interface{}) {

	routeUpdate := routeUpdateI.(*RouteUpdate)

	cache.routesCache.mutex.Lock()
	defer cache.routesCache.mutex.Unlock()

	updateType := routeUpdate.Type
	objectNamespace := routeUpdate.RouteObject.Metadata.Namespace

	if cache.routesCache == nil {
		logger.ErrorC(ctx, "Cache of routes is nil unexpectedly. Stack: %s", runtime.StartTrace())
		return
	}
	if cache.routesCache.routes == nil {
		logger.ErrorC(ctx, "Map of routes in cache is nil unexpectedly. Stack: %s", runtime.StartTrace())
		return
	}

	logger.DebugC(ctx, "Update cache routes with route update %s for namespace %s", routeUpdate, objectNamespace)
	if updateType == updateTypeInit {
		logger.DebugC(ctx, "Type is %s, init route cache for namespace '%s'", updateType, objectNamespace)
		cache.routesCache.mutex.Unlock()
		cache.routesCache.initCache(ctx, objectNamespace)
		cache.routesCache.mutex.Lock()
	} else {

		namespacedRoutesPtr, ok := cache.routesCache.routes[objectNamespace]
		if !ok {
			logger.ErrorC(ctx, "During update namespace %s was not found in routes cache", objectNamespace)
			return
		}
		namespacedRoutes := *namespacedRoutesPtr
		if namespacedRoutes == nil {
			logger.ErrorC(ctx, "Map of namespaced routes in cache is nil unexpectedly. Stack: %s", runtime.StartTrace())
			return
		}

		objectName := routeUpdate.RouteObject.Metadata.Name

		if updateType == updateTypeCreated || updateType == updateTypeAdded || updateType == updateTypeModified {
			logger.DebugC(ctx, "Type is %s, renew route %s with namespace %s in cache", updateType, objectName, objectNamespace)
			namespacedRoutes[objectName] = routeUpdate.RouteObject
		} else if updateType == updateTypeDeleted {
			logger.DebugC(ctx, "Type is DELETED, remove route %s from cache from namespace %s", objectName, objectNamespace)
			delete(namespacedRoutes, objectName)
		}
	}
}

func (cache *CompositeCache) updateServicesCache(ctx context.Context, serviceUpdateI interface{}) {

	serviceUpdate := serviceUpdateI.(*ServiceUpdate)

	cache.servicesCache.mutex.Lock()
	defer cache.servicesCache.mutex.Unlock()

	updateType := serviceUpdate.Type
	objectNamespace := serviceUpdate.ServiceObject.Metadata.Namespace

	if cache.servicesCache == nil {
		logger.ErrorC(ctx, "Cache of services is nil unexpectedly. Stack: %s", runtime.StartTrace())
		return
	}
	if cache.servicesCache.services == nil {
		logger.ErrorC(ctx, "Map of services in cache is nil unexpectedly. Stack: %s", runtime.StartTrace())
		return
	}

	logger.DebugC(ctx, "Update services cache with service update %s for namespace %s", serviceUpdate, objectNamespace)
	if updateType == updateTypeInit {
		logger.DebugC(ctx, "Type is %s, init services cache for namespace '%s'", updateType, objectNamespace)
		cache.servicesCache.mutex.Unlock()
		cache.servicesCache.initCache(ctx, objectNamespace)
		cache.servicesCache.mutex.Lock()
	} else {

		namespacedServicesPtr, ok := cache.servicesCache.services[objectNamespace]
		if !ok {
			logger.ErrorC(ctx, "During update namespace %s was not found in services cache", objectNamespace)
			return
		}
		namespacedServices := *namespacedServicesPtr
		if namespacedServices == nil {
			logger.ErrorC(ctx, "Map of namespaced services in cache is nil unexpectedly. Stack: %s", runtime.StartTrace())
			return
		}

		objectName := serviceUpdate.ServiceObject.Metadata.Name

		if updateType == updateTypeCreated || updateType == updateTypeAdded || updateType == updateTypeModified {
			logger.DebugC(ctx, "Type is %s, renew service %s with namespace %s in cache", updateType, objectName, objectNamespace)
			namespacedServices[objectName] = serviceUpdate.ServiceObject
		} else if updateType == updateTypeDeleted {
			logger.DebugC(ctx, "Type is DELETED, remove service %s from cache from namespace %s", objectName, objectNamespace)
			delete(namespacedServices, objectName)
		}
	}
}

func (cache *CompositeCache) updateConfigmapsCache(ctx context.Context, configMapUpdateI interface{}) {

	configMapUpdate := configMapUpdateI.(*ConfigMapUpdate)

	cache.configMapsCache.mutex.Lock()
	defer cache.configMapsCache.mutex.Unlock()

	updateType := configMapUpdate.Type
	objectNamespace := configMapUpdate.ConfigMapObject.Metadata.Namespace

	if cache.configMapsCache == nil {
		logger.ErrorC(ctx, "Cache of ConfigMaps is nil unexpectedly. Stack: %s", runtime.StartTrace())
		return
	}
	if cache.configMapsCache.configMaps == nil {
		logger.ErrorC(ctx, "Map of ConfigMaps in cache is nil unexpectedly. Stack: %s", runtime.StartTrace())
		return
	}

	logger.DebugC(ctx, "Update cache ConfigMaps with configMap update %s for namespace %s of type %s", configMapUpdate, objectNamespace)
	if updateType == updateTypeInit {
		logger.DebugC(ctx, "Type is %s, init ConfigMaps cache for namespace '%s'", updateType, objectNamespace)
		cache.configMapsCache.mutex.Unlock()
		cache.configMapsCache.initCache(ctx, objectNamespace)
		cache.configMapsCache.mutex.Lock()
	} else {

		namespacedConfigMapPtr, ok := cache.configMapsCache.configMaps[objectNamespace]
		if !ok {
			logger.ErrorC(ctx, "During update namespace %s was not found in ConfigMaps cache", objectNamespace)
		}
		namespacedConfigMap := *namespacedConfigMapPtr
		if namespacedConfigMap == nil {
			logger.ErrorC(ctx, "Map of namespaced ConfigMaps in cache is nil unexpectedly. Stack: %s", runtime.StartTrace())
		}

		objectName := configMapUpdate.ConfigMapObject.Metadata.Name
		// Site-management uses only 2 configmaps, we don't need to process all configmaps
		if !FilterRequiredConfigMaps(objectName) {
			return
		}

		if updateType == updateTypeCreated || updateType == updateTypeAdded || updateType == updateTypeModified {
			logger.DebugC(ctx, "Type is %s, renew ConfigMaps %s with namespace in cache", updateType, objectName, objectNamespace)
			namespacedConfigMap[objectName] = configMapUpdate.ConfigMapObject
		} else if updateType == updateTypeDeleted {
			logger.DebugC(ctx, "Type is DELETED, remove ConfigMap %s from cache from namespace %s", objectName, objectNamespace)
			delete(namespacedConfigMap, objectName)
		}
	}
}

func (c *PaasMediationClient) StartSyncingCache(ctx context.Context) {

	routeUpd := CommonUpdateStr{
		updateCache: func(ctx context.Context, routeUpdateI interface{}) {
			routeUpdate := routeUpdateI.(*RouteUpdate)
			c.cache.updateRoutesCache(ctx, routeUpdateI)
			for _, callback := range c.callbacks {
				err := callback(ctx, c, *routeUpdate)
				if err != nil {
					logger.ErrorC(ctx, "Failed to execute callback function to notify Routes changes")
					if stackErr, ok := err.(*errors.Error); ok {
						logger.ErrorC(ctx, "Stack: %s", stackErr.ErrorStack())
					}
				}
			}
		},
		resource:   RouteUpdate{},
		bus:        c.cache.routesCache.bus,
		lastUpdate: make(chan time.Time),
	}
	serviceUpd := CommonUpdateStr{c.cache.updateServicesCache, ServiceUpdate{}, c.cache.servicesCache.bus, make(chan time.Time)}
	configMapUpd := CommonUpdateStr{c.cache.updateConfigmapsCache, ConfigMapUpdate{}, c.cache.configMapsCache.bus, make(chan time.Time)}
	commonUpdates := []CommonUpdateStr{routeUpd, serviceUpd, configMapUpd}

	for _, cb := range commonUpdates {
		go syncingCache(ctx, cb)
	}

	for _, update := range commonUpdates {
		go c.keepActualLastUpdateTime(update)
	}
}

// keepActualLastUpdateTime updates composite cache last update time
func (c *PaasMediationClient) keepActualLastUpdateTime(update CommonUpdateStr) {
	for {
		lastUpdate := <-update.lastUpdate
		c.cache.lastCacheUpdateTime = lastUpdate
	}
}

func syncingCache(ctx context.Context, str CommonUpdateStr) {
	syncingCacheInternal(ctx, str, sleepWs)
}

func syncingCacheInternal(ctx context.Context, cb CommonUpdateStr, sleepWs time.Duration) {
	for attemptNumber := attemptsWs; attemptNumber > 0; attemptNumber-- {
		func() {
			defer func() {
				if err := recover(); err != nil {
					logger.Error("panic occurred: %s, stack:\n %s", err, string(debug.Stack()))
				}
			}()
			for {
				update := <-cb.bus
				res := reflect.New(reflect.TypeOf(cb.resource)).Interface()
				err := json.Unmarshal(update, res)
				if err != nil {
					logger.ErrorC(ctx, "Error while unmarshalling update body: %s", err)
					continue
				}
				logger.DebugC(ctx, "Start updating cache with update: %s", res)
				cb.updateCache(ctx, res)
				cb.lastUpdate <- time.Now()
				logger.DebugC(ctx, "Syncing cache for openshift resources completed successfully")
				attemptNumber = attemptsWs
			}
		}()
		time.Sleep(sleepWs)
	}
	panic("Used all attempts to read channel")
}

func (c *PaasMediationClient) getRoutesWithoutCache(ctx context.Context, namespace string) (*[]domain.Route, error) {
	logger.InfoC(ctx, "Get list of routes from namespace %s", namespace)
	buildUrl, err := c.buildUrl(ctx, namespace, routesString, "")
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while building route list url: %+v", err)
		return nil, err
	}
	var routeList = new([]domain.Route)
	err = c.performRequestWithRetry(ctx, buildUrl, fasthttp.MethodGet, nil, fasthttp.StatusOK, routeList)
	if err != nil {
		return nil, err
	}
	logger.InfoC(ctx, "Get routes from namespace %s was completed successfully. Got %d routes", namespace, len(*routeList))
	return routeList, nil
}

// Build paas mediation url.
// Url can be the following types:
// 1) {paas mediation host}/api/v1/namespaces/{namespace}/{resources type}
// 2) {paas mediation host}/api/v1/namespaces/{namespace}/{resources type}/{resource name}
// Required parameters: namespace and resourceType. Optional parameter: resourceName
func (c *PaasMediationClient) buildUrl(ctx context.Context, namespace, resourceType, resourceName string) (string, error) {
	if namespace == "" || resourceType == "" {
		return "", fmt.Errorf("namespace and resourceType parameters can not be empty")
	}
	requestedUrl := fmt.Sprintf("%s/api/v2/paas-mediation/namespaces/%s/%s", c.InternalGatewayAddress, namespace, resourceType)
	if resourceName != "" {
		requestedUrl += "/" + resourceName
	}
	logger.DebugC(ctx, "Url for paas mediation was built: {}", requestedUrl)
	return requestedUrl, nil
}

func (c *PaasMediationClient) getServicesWithoutCache(ctx context.Context, namespace string) (*[]domain.Service, error) {
	logger.InfoC(ctx, "Get list of services from namespace %s", namespace)
	buildUrl, err := c.buildUrl(ctx, namespace, servicesString, "")
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while building service list url: %+v", err)
		return nil, err
	}
	var serviceList = new([]domain.Service)
	err = c.performRequestWithRetry(ctx, buildUrl, fasthttp.MethodGet, nil, fasthttp.StatusOK, serviceList)
	if err != nil {
		return nil, err
	}
	logger.InfoC(ctx, "Get services from namespace %s was completed successfully. Got %d services", namespace, len(*serviceList))
	return serviceList, nil
}

// Site-management uses only 2 configmaps, we don't need to cache all configmaps
func (c *PaasMediationClient) getConfigMapsWithoutCache(ctx context.Context, namespace string) (*[]domain.Configmap, error) {
	logger.InfoC(ctx, "Get configmap list from namespace %s", namespace)
	buildUrl, err := c.buildUrl(ctx, namespace, configmapsString, "")
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while building configmap list url: %+v", err)
		return nil, err
	}
	var allConfigMaps = new([]domain.Configmap)
	err = c.performRequestWithRetry(ctx, buildUrl, fasthttp.MethodGet, nil, fasthttp.StatusOK, allConfigMaps)
	if err != nil {
		return nil, err
	}
	logger.InfoC(ctx, "Get configmaps from namespace %s was completed successfully. Got %d configmaps", namespace, len(*allConfigMaps))

	var requiredConfigMaps = new([]domain.Configmap)
	for _, configmap := range *allConfigMaps {
		if FilterRequiredConfigMaps(configmap.Metadata.Name) {
			logger.InfoC(ctx, "Found required configmap %s", configmap.Metadata.Name)
			*requiredConfigMaps = append(*requiredConfigMaps, configmap)
		}
	}
	return requiredConfigMaps, nil
}

func FilterRequiredConfigMaps(configMapName string) bool {
	if configMapName == ProjectTypeConfigMapName || configMapName == TMConfigsConfigmapName {
		return true
	}
	return false
}

func (c *PaasMediationClient) GetAllRoutesFromCurrentNamespace(ctx context.Context) ([]domain.Route, error) {
	routes, err := c.GetRoutes(ctx, c.Namespace)
	if err != nil {
		return nil, errors.Wrap(err, 1)
	}
	return *routes, err
}

func (c *PaasMediationClient) GetRoutes(ctx context.Context, namespace string) (*[]domain.Route, error) {
	if namespace == "" {
		namespace = c.Namespace
	}
	logger.InfoC(ctx, "Get routes from namespace %s", namespace)

	c.cache.routesCache.mutex.RLock()
	defer c.cache.routesCache.mutex.RUnlock()

	localCache, ok := c.cache.routesCache.routes[namespace]
	if !ok {
		logger.WarnC(ctx, "Namespace %s was not found in cache, trying to get routes from paas-mediation service", namespace)
		c.cache.routesCache.mutex.RUnlock()
		CreateWebSocketClient(ctx, &c.cache.routesCache.bus, c.InternalGatewayAddress.Host, namespace, routesString)
		for i := 0; ; i++ {
			time.Sleep(sleepInit)
			c.cache.routesCache.mutex.RLock()
			if localCache, ok = c.cache.routesCache.routes[namespace]; !ok {
				if i >= attemptsInit {
					err := fmt.Errorf("Namespace %s was not found in routes cache after %v attempts", namespace, attemptsInit)
					logger.ErrorC(ctx, err.Error())
					return nil, err
				}
				c.cache.routesCache.mutex.RUnlock()
			} else {
				break
			}
		}
	}
	logger.InfoC(ctx, "Return %d routes from cache of namespace %s", len(*localCache), namespace)
	result := make([]domain.Route, 0, len(*localCache))
	for _, route := range *localCache {
		result = append(result, route)
	}
	logger.InfoC(ctx, "Result was built successfully")
	return &result, nil
}

func (c *PaasMediationClient) initRoutesMapInCache(ctx context.Context, namespace string) {
	logger.InfoC(ctx, "Initialization routes map in cache of namespace %s", namespace)
	if routes, err := c.getRoutesWithoutCache(ctx, namespace); err == nil {

		c.cache.routesCache.mutex.Lock()
		defer c.cache.routesCache.mutex.Unlock()

		initialNamespace := make(map[string]domain.Route)
		c.cache.routesCache.routes[namespace] = &initialNamespace
		for _, route := range *routes {
			(*c.cache.routesCache.routes[namespace])[route.Metadata.Name] = route
		}
		logger.InfoC(ctx, "Return %d routes of namespace %s", len(*routes), namespace)
	} else {
		logger.ErrorC(ctx, "Error occurred while getting routes from paas-mediation: %s", err.Error())
	}
}

func (c *PaasMediationClient) GetServices(ctx context.Context, namespace string) (*[]domain.Service, error) {
	if namespace == "" {
		namespace = c.Namespace
	}
	logger.InfoC(ctx, "Get services from namespace %s", namespace)

	c.cache.servicesCache.mutex.RLock()
	defer c.cache.servicesCache.mutex.RUnlock()

	localCache, ok := c.cache.servicesCache.services[namespace]
	if !ok {
		logger.WarnC(ctx, "Namespace %s was not found in cache, trying to get services from paas-mediation service", namespace)
		c.cache.servicesCache.mutex.RUnlock()
		CreateWebSocketClient(ctx, &c.cache.servicesCache.bus, c.InternalGatewayAddress.Host, namespace, servicesString)
		for i := 0; ; i++ {
			time.Sleep(sleepInit)
			c.cache.servicesCache.mutex.RLock()
			if localCache, ok = c.cache.servicesCache.services[namespace]; !ok {
				if i >= attemptsInit {
					err := fmt.Errorf("Namespace %s was not found in services cache after %v attempts", namespace, attemptsInit)
					logger.ErrorC(ctx, err.Error())
					return nil, err
				}
				c.cache.servicesCache.mutex.RUnlock()
			} else {
				break
			}
		}
	}
	logger.InfoC(ctx, "Return %d services from cache of namespace %s", len(*localCache), namespace)
	result := make([]domain.Service, 0, len(*localCache))
	for _, route := range *localCache {
		result = append(result, route)
	}
	logger.InfoC(ctx, "Result was built successfully")
	return &result, nil
}

func (c *PaasMediationClient) InitServicesMapInCache(ctx context.Context, namespace string) {
	logger.InfoC(ctx, "Initialization services map in cache of namespace %s", namespace)
	if services, err := c.getServicesWithoutCache(ctx, namespace); err == nil {

		c.cache.servicesCache.mutex.Lock()
		defer c.cache.servicesCache.mutex.Unlock()

		initialNamespace := make(map[string]domain.Service)
		c.cache.servicesCache.services[namespace] = &initialNamespace
		for _, service := range *services {
			(*c.cache.servicesCache.services[namespace])[service.Metadata.Name] = service
		}
		logger.InfoC(ctx, "Return %d services of namespace %s", len(*services), namespace)
	} else {
		logger.ErrorC(ctx, "Error occurred while getting services from paas-mediation: %s", err.Error())
	}
}

func (c *PaasMediationClient) GetConfigMaps(ctx context.Context, namespace string) (*[]domain.Configmap, error) {
	if namespace == "" {
		namespace = c.Namespace
	}
	logger.InfoC(ctx, "Get ConfigMaps from namespace %s", namespace)

	c.cache.configMapsCache.mutex.RLock()
	defer c.cache.configMapsCache.mutex.RUnlock()

	localCache, ok := c.cache.configMapsCache.configMaps[namespace]
	if !ok {
		logger.WarnC(ctx, "Namespace %s was not found in cache, trying to get ConfigMaps from paas-mediation service", namespace)
		c.cache.configMapsCache.mutex.RUnlock()
		CreateWebSocketClient(ctx, &c.cache.configMapsCache.bus, c.InternalGatewayAddress.Host, namespace, configmapsString)
		for i := 0; ; i++ {
			time.Sleep(sleepInit)
			c.cache.configMapsCache.mutex.RLock()
			if localCache, ok = c.cache.configMapsCache.configMaps[namespace]; !ok {
				if i >= attemptsInit {
					err := fmt.Errorf("Namespace %s was not found in ConfigMaps cache after %v attempts", namespace, attemptsInit)
					logger.ErrorC(ctx, err.Error())
					return nil, err
				}
				c.cache.configMapsCache.mutex.RUnlock()
			} else {
				break
			}
		}
	}
	logger.InfoC(ctx, "Return %d ConfigMaps from cache of namespace %s", len(*localCache), namespace)
	result := make([]domain.Configmap, 0, len(*localCache))
	for _, route := range *localCache {
		result = append(result, route)
	}
	logger.InfoC(ctx, "Result was built successfully")
	return &result, nil
}

func (c *PaasMediationClient) initConfigMapsMapInCache(ctx context.Context, namespace string) {
	logger.InfoC(ctx, "Initialization ConfigMaps map in cache of namespace %s", namespace)
	if configMaps, err := c.getConfigMapsWithoutCache(ctx, namespace); err == nil {

		c.cache.configMapsCache.mutex.Lock()
		defer c.cache.configMapsCache.mutex.Unlock()

		initialNamespace := make(map[string]domain.Configmap)
		c.cache.configMapsCache.configMaps[namespace] = &initialNamespace
		for _, configMap := range *configMaps {
			(*c.cache.configMapsCache.configMaps[namespace])[configMap.Metadata.Name] = configMap
		}
		logger.InfoC(ctx, "Return %d ConfigMaps of namespace %s", len(*configMaps), namespace)
	} else {
		logger.ErrorC(ctx, "Error occurred while getting ConfigMaps from paas-mediation: %s", err.Error())
	}
}

func (c *PaasMediationClient) GetRoutesByFilter(ctx context.Context, namespace string, filter func(*domain.Route) bool) (*[]domain.Route, error) {
	if routes, err := c.GetRoutes(ctx, namespace); err == nil {
		result := make([]domain.Route, 0)
		for _, route := range *routes {
			if filter(&route) {
				result = append(result, route)
			}
		}
		return &result, nil
	} else {
		return nil, err
	}
}

func (c *PaasMediationClient) GetServices2(ctx context.Context, namespace string, filter func(*domain.Service) bool) (*[]domain.Service, error) {
	if services, err := c.GetServices(ctx, namespace); err == nil {
		filtered := make([]domain.Service, 0)
		for _, service := range *services {
			if filter(&service) {
				filtered = append(filtered, service)
			}
		}
		return &filtered, nil
	} else {
		return nil, err
	}
}

func (c *PaasMediationClient) GetConfigMaps2(ctx context.Context, namespace string, filter func(*domain.Configmap) bool) (*[]domain.Configmap, error) {
	if configmaps, err := c.GetConfigMaps(ctx, namespace); err == nil {
		result := make([]domain.Configmap, 0)
		for _, configmap := range *configmaps {
			if filter(&configmap) {
				result = append(result, configmap)
			}
		}
		return &result, nil
	} else {
		return nil, err
	}
}

func (c *PaasMediationClient) GetRoutesForNamespaces(ctx context.Context, namespaces []string) (*[]domain.Route, error) {
	result := make([]domain.Route, 0)
	for _, namespace := range namespaces {
		routes, err := c.GetRoutes(ctx, namespace)
		if err != nil {
			logger.ErrorC(ctx, "Error during get routes from paas-mediation")
			return nil, err
		}
		result = append(result, *routes...)
	}
	return &result, nil
}

func (c *PaasMediationClient) GetRoutesForNamespaces2(ctx context.Context, namespaces []string, filter func(*domain.Route) bool) (*[]domain.Route, error) {
	if routes, err := c.GetRoutesForNamespaces(ctx, namespaces); err == nil {
		result := make([]domain.Route, 0)
		for _, route := range *routes {
			if filter(&route) {
				result = append(result, route)
			}
		}
		return &result, nil
	} else {
		return nil, err
	}
}

func (c *PaasMediationClient) GetServicesForNamespaces(ctx context.Context, namespaces []string) (*[]domain.Service, error) {
	result := make([]domain.Service, 0)
	for _, namespace := range namespaces {
		services, err := c.GetServices(ctx, namespace)
		if err != nil {
			return nil, err
		}
		result = append(result, *services...)
	}
	return &result, nil
}

func (c *PaasMediationClient) GetServicesForNamespaces2(ctx context.Context, namespaces []string, filter func(*domain.Service) bool) (*[]domain.Service, error) {
	if services, err := c.GetServicesForNamespaces(ctx, namespaces); err == nil {
		result := make([]domain.Service, 0)
		for _, service := range *services {
			if filter(&service) {
				result = append(result, service)
			}
		}
		return &result, nil
	} else {
		return nil, err
	}
}

func (c *PaasMediationClient) DeleteRoute(ctx context.Context, namespace, name string) error {
	logger.InfoC(ctx, "Delete route %s from namespace %s", name, namespace)

	buildUrl, err := c.buildUrl(ctx, namespace, routesString, name)
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while building url for delete route with name=%s: %+v", name, err)
		return wrappers.ErrorWrapper{StatusCode: http.StatusBadRequest, Message: err.Error()}
	}
	var result = new(map[string]interface{})
	if err = c.performRequest(ctx, buildUrl, fasthttp.MethodDelete, nil, fasthttp.StatusOK, result); err != nil {
		logger.ErrorC(ctx, "Error occurred while deleting route with name=%s in namespace=%s: %s", name, namespace, err)
		return err
	}
	logger.InfoC(ctx, "Route %s was successfully removed from namespace %s, result %+v", name, namespace, result)
	routeToDelete := RouteUpdate{
		Type: updateTypeDeleted,
		RouteObject: domain.Route{
			Metadata: domain.Metadata{
				Namespace: namespace,
				Name:      name,
			},
		},
	}
	c.cache.updateRoutesCache(ctx, &routeToDelete)
	return nil
}

func (c *PaasMediationClient) CreateRoute(ctx context.Context, route *domain.Route, namespace string) error {
	logger.InfoC(ctx, "Create route %s for namespace %s", route, namespace)
	buildUrl, err := c.buildUrl(ctx, namespace, routesString, "")
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while building url for create route in namespace=%s: %+v", namespace, err)
		return wrappers.ErrorWrapper{StatusCode: http.StatusBadRequest, Message: err.Error()}
	}
	routeHostToLowerCase(route)
	body, err := json.Marshal(route)
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while marshalling route into json: %s", err)
		return wrappers.ErrorWrapper{StatusCode: http.StatusInternalServerError, Message: err.Error()}
	}
	logger.DebugC(ctx, "Create route: %s", body)
	if err = c.performRequest(ctx, buildUrl, fasthttp.MethodPost, body, fasthttp.StatusCreated, route); err != nil {
		logger.ErrorC(ctx, "Error occurred while creating route in namespace=%s. \n\tRoute: \n%v\n\tError: %v", namespace, route.FormatString("\t\t"), err)
		return err
	}
	logger.InfoC(ctx, "Route %s was successfully created in namespace %s. Result: %+v", route.Metadata.Name, namespace, route)
	c.cache.updateRoutesCache(ctx, &RouteUpdate{Type: updateTypeCreated, RouteObject: *route})
	return nil
}

func (c *PaasMediationClient) CreateService(ctx context.Context, service *domain.Service, namespace string) error {
	logger.InfoC(ctx, "Create service %s for namespace %s", service, namespace)
	buildUrl, err := c.buildUrl(ctx, namespace, servicesString, "")
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while building url for creating service in namespace=%s: %+v", namespace, err)
		return wrappers.ErrorWrapper{StatusCode: http.StatusBadRequest, Message: err.Error()}
	}
	body, err := json.Marshal(service)
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while marshalling service into json: %s", err)
		return wrappers.ErrorWrapper{StatusCode: http.StatusInternalServerError, Message: err.Error()}
	}
	logger.DebugC(ctx, "Create service: %s", body)
	if err = c.performRequest(ctx, buildUrl, fasthttp.MethodPost, body, fasthttp.StatusCreated, service); err != nil {
		logger.ErrorC(ctx, "Error occurred while creating service in namespace=%s. \n\tService: \n%v\n\tError: %v", service, err)
		return err
	}
	logger.InfoC(ctx, "Service %s was successfully created in namespace %s. Result: %+v", service.Metadata.Name, namespace, service)
	c.cache.updateServicesCache(ctx, &ServiceUpdate{Type: updateTypeCreated, ServiceObject: *service})
	return nil
}

func (c *PaasMediationClient) UpdateOrCreateRoute(ctx context.Context, route *domain.Route, namespace string) error {
	logger.InfoC(ctx, "Update route %s for namespace %s", route, namespace)
	buildUrl, err := c.buildUrl(ctx, namespace, routesString, "")
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while building url for update route in namespace=%s: %+v", namespace, err)
		return wrappers.ErrorWrapper{StatusCode: http.StatusBadRequest, Message: err.Error()}
	}
	routeHostToLowerCase(route)
	body, err := json.Marshal(route)
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while marshalling route into json: %s", err)
		return wrappers.ErrorWrapper{StatusCode: http.StatusInternalServerError, Message: err.Error()}
	}
	logger.DebugC(ctx, "Update route: %s", body)
	if err = c.performRequest(ctx, buildUrl, fasthttp.MethodPut, body, fasthttp.StatusOK, route); err != nil {
		logger.ErrorC(ctx, "Error occurred while updating route in namespace=%s. \n\tRoute: \n%v\n\tError: %v", route.FormatString("\t\t"), err)
		return err
	}
	logger.InfoC(ctx, "Route %s was successfully updated in namespace %s. Result: %+v", route.Metadata.Name, namespace, route)
	c.cache.updateRoutesCache(ctx, &RouteUpdate{Type: updateTypeModified, RouteObject: *route})
	return nil
}

func (c *PaasMediationClient) UpdateOrCreateService(ctx context.Context, service *domain.Service, namespace string) error {
	logger.InfoC(ctx, "Update service %s for namespace %s", service, namespace)
	buildUrl, err := c.buildUrl(ctx, namespace, servicesString, "")
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while building url for updating service in namespace=%s: %+v", namespace, err)
		return wrappers.ErrorWrapper{StatusCode: http.StatusBadRequest, Message: err.Error()}
	}
	body, err := json.Marshal(service)
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while marshalling service into json: %s", err)
		return wrappers.ErrorWrapper{StatusCode: http.StatusInternalServerError, Message: err.Error()}
	}
	logger.DebugC(ctx, "Update service: %s", body)
	if err = c.performRequest(ctx, buildUrl, fasthttp.MethodPut, body, fasthttp.StatusOK, service); err != nil {
		logger.ErrorC(ctx, "Error occurred while updating service in namespace=%s. \n\tService: \n%v\n\tError: %v", service, err)
		return err
	}
	logger.InfoC(ctx, "Service %s was successfully updated in namespace %s. Result: %+v", service.Metadata.Name, namespace, service)
	c.cache.updateServicesCache(ctx, &ServiceUpdate{Type: updateTypeModified, ServiceObject: *service})
	return nil
}

func (c *PaasMediationClient) DeleteService(ctx context.Context, service, namespace string) error {
	logger.InfoC(ctx, "Delete service %s for namespace %s", service, namespace)
	buildUrl, err := c.buildUrl(ctx, namespace, servicesString, service)
	if err != nil {
		logger.ErrorC(ctx, "Error occurred while building url for deleting service in namespace=%s: %+v", namespace, err)
		return wrappers.ErrorWrapper{StatusCode: http.StatusBadRequest, Message: err.Error()}
	}
	var result = new(map[string]interface{})
	if err = c.performRequest(ctx, buildUrl, fasthttp.MethodDelete, nil, fasthttp.StatusOK, result); err != nil {
		logger.ErrorC(ctx, "Error occurred while deleting service with name=%s in namespace=%s: %s", service, namespace, err)
		return err
	}
	logger.InfoC(ctx, "Service %s was successfully removed from namespace %s, result %+v", service, namespace, result)
	serviceToDelete := ServiceUpdate{
		Type: updateTypeDeleted,
		ServiceObject: domain.Service{
			Metadata: domain.Metadata{
				Name:      service,
				Namespace: namespace,
			},
		},
	}
	c.cache.updateServicesCache(ctx, &serviceToDelete)
	return nil
}

func (ex *httpExecutorImpl) doRequest(ctx context.Context, url, method string, body []byte) (*fasthttp.Response, error) {
	return utils.DoRequest(ctx, method, url, body, logger)
}

// responseBody must be pointer
func (c *PaasMediationClient) performRequest(ctx context.Context, url, method string, body []byte, expectedCode int, responseBody interface{}) error {
	logger.InfoC(ctx, "Perform request to paas mediation with url '%s', method '%s'", url, method)
	resp, err := c.httpExecutor.doRequest(ctx, url, method, body)
	if err != nil {
		logger.ErrorC(ctx, "Error while performing request: %s", err)
		return wrappers.ErrorWrapper{StatusCode: http.StatusInternalServerError, Message: err.Error()}
	}
	defer fasthttp.ReleaseResponse(resp)

	logger.DebugC(ctx, "Got %d status code in response", resp.StatusCode())
	if resp.StatusCode() != expectedCode {
		return wrappers.ErrorWrapper{StatusCode: resp.StatusCode(), Message: fmt.Sprintf("Found %d instead of %d while performing paas mediation request. Returned error message: %s", resp.StatusCode(), expectedCode, string(resp.Body()))}
	}
	if err := c.decodeResponseBody(ctx, resp.Body(), responseBody); err != nil {
		return wrappers.ErrorWrapper{StatusCode: http.StatusInternalServerError, Message: err.Error()}
	}
	logger.DebugC(ctx, "Request was successful, result: %+v", responseBody)
	return nil
}

func (c *PaasMediationClient) performRequestWithRetry(ctx context.Context, url, method string, body []byte, code int, responseBody interface{}) error {
	var err error
	for i := attempts; i > 0; i-- {
		err := c.performRequest(ctx, url, method, body, code, responseBody)
		if err == nil {
			return nil
		}
		logger.ErrorC(ctx, "Error occurred getting resource %s, attempt %v/%v : %s", url, i, attempts, err)
		time.Sleep(period)
	}
	return err
}

func (c *PaasMediationClient) decodeResponseBody(ctx context.Context, body []byte, responseBody interface{}) error {
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(responseBody); err != nil && err != io.EOF {
		logger.ErrorC(ctx, "Response body can't be parsed: %+v", err)
		return err
	}
	return nil
}

func (c *PaasMediationClient) initCompositeCache(ctx context.Context) {
	chRoutes := make(chan *RoutesCache)
	chServices := make(chan *ServicesCache)
	chConfigMaps := make(chan *ConfigMapsCache)

	go c.initRoutesCache(ctx, chRoutes)
	go c.initServicesCache(ctx, chServices)
	go c.initConfigMapsCache(ctx, chConfigMaps)

	routesCache := <-chRoutes
	servicesCache := <-chServices
	configMapsCache := <-chConfigMaps

	c.cache = &CompositeCache{
		routesCache:         routesCache,
		servicesCache:       servicesCache,
		configMapsCache:     configMapsCache,
		lastCacheUpdateTime: time.Now(),
	}
}

func (c *PaasMediationClient) initRoutesCache(ctx context.Context, ch chan *RoutesCache) {
	newMutex := &sync.RWMutex{}
	initialRoutes := make(map[string]*map[string]domain.Route)
	initialNamespace := make(map[string]domain.Route)
	initialRoutes[c.Namespace] = &initialNamespace
	routes, err := c.getRoutesWithoutCache(ctx, c.Namespace)
	if err != nil {
		logger.ErrorC(ctx, "Failed to initialize cache of openshift routes: %s", err)
		panic(err)
	}
	for _, route := range *routes {
		(*initialRoutes[c.Namespace])[route.Metadata.Name] = route
	}

	channel := make(chan []byte, 50)
	CreateWebSocketClient(ctx, &channel, c.InternalGatewayAddress.Host, c.Namespace, routesString)

	routesCache := RoutesCache{
		mutex:     newMutex,
		routes:    initialRoutes,
		bus:       channel,
		initCache: c.initRoutesMapInCache,
	}

	ch <- &routesCache
}

func (c *PaasMediationClient) initServicesCache(ctx context.Context, ch chan *ServicesCache) {
	newMutex := &sync.RWMutex{}
	initialServices := make(map[string]*map[string]domain.Service)
	initialNamespace := make(map[string]domain.Service)
	initialServices[c.Namespace] = &initialNamespace
	services, err := c.getServicesWithoutCache(ctx, c.Namespace)
	if err != nil {
		logger.ErrorC(ctx, "Failed to initialize cache for openshift services: %s", err)
		panic(err)
	}
	for _, service := range *services {
		(*initialServices[c.Namespace])[service.Metadata.Name] = service
	}

	channel := make(chan []byte, 50)
	CreateWebSocketClient(ctx, &channel, c.InternalGatewayAddress.Host, c.Namespace, servicesString)

	servicesCache := ServicesCache{
		mutex:     newMutex,
		services:  initialServices,
		bus:       channel,
		initCache: c.InitServicesMapInCache,
	}

	ch <- &servicesCache
}

func (c *PaasMediationClient) initConfigMapsCache(ctx context.Context, ch chan *ConfigMapsCache) {
	newMutex := &sync.RWMutex{}
	initialConfigMaps := make(map[string]*map[string]domain.Configmap)
	initialNamespace := make(map[string]domain.Configmap)
	initialConfigMaps[c.Namespace] = &initialNamespace
	configmaps, err := c.getConfigMapsWithoutCache(ctx, c.Namespace)
	if err != nil {
		logger.ErrorC(ctx, "Failed to initialize cache for openshift configmaps: %s", err)
		panic(err)
	}
	for _, configMap := range *configmaps {
		(*initialConfigMaps[c.Namespace])[configMap.Metadata.Name] = configMap
	}

	channel := make(chan []byte, 50)
	CreateWebSocketClient(ctx, &channel, c.InternalGatewayAddress.Host, c.Namespace, configmapsString)

	configMapsCache := ConfigMapsCache{
		mutex:      newMutex,
		configMaps: initialConfigMaps,
		bus:        channel,
		initCache:  c.initConfigMapsMapInCache,
	}

	ch <- &configMapsCache
}

func (c *PaasMediationClient) GetLastCacheUpdateTime() time.Time {
	return c.cache.lastCacheUpdateTime
}

// for compatibility with old kubernetes
func routeHostToLowerCase(route *domain.Route) {
	route.Spec.Host = strings.ToLower(route.Spec.Host)
}
