package paasMediationClient

import "github.com/netcracker/qubership-core-site-management/site-management-service/v2/paasMediationClient/domain"

const (
	updateTypeModified = "MODIFIED"
	updateTypeCreated  = "CREATED"
	updateTypeAdded    = "ADDED"
	updateTypeDeleted  = "DELETED"
	updateTypeInit     = "INIT"
)

type RouteUpdate struct {
	Type        string       `json:"type"`
	RouteObject domain.Route `json:"object"`
}

type ServiceUpdate struct {
	Type          string         `json:"type"`
	ServiceObject domain.Service `json:"object"`
}

type ConfigMapUpdate struct {
	Type            string           `json:"type"`
	ConfigMapObject domain.Configmap `json:"object"`
}

type CommonUpdate struct {
	Type         string              `json:"type"`
	CommonObject domain.CommonObject `json:"object"`
}
