package domain

import "github.com/netcracker/qubership-core-site-management/site-management-service/v2/utils"

type Service struct {
	Metadata Metadata    `json:"metadata"`
	Spec     ServiceSpec `json:"spec"`
}

type ServiceSpec struct {
	Ports     []Port            `json:"ports"`
	Selector  map[string]string `json:"selector"`
	ClusterIP string            `json:"clusterIP"`
	Type      string            `json:"type"`
}

type Port struct {
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	Port       int32  `json:"port"`
	TargetPort int32  `json:"targetPort"`
	NodePort   int32  `json:"nodePort,omitempty"`
}

func (s Service) GetId() string {
	return utils.FindAnnotation(s.Metadata.Annotations, "tenant.service.id")
}

func (s Service) GetShowName() string {
	return utils.FindAnnotation(s.Metadata.Annotations, "tenant.service.show.name")
}

func (s Service) GetDescription() string {
	return utils.FindAnnotation(s.Metadata.Annotations, "tenant.service.show.description")
}

func (s Service) GetSuffix() string {
	return utils.FindAnnotation(s.Metadata.Annotations, "tenant.service.url.suffix")
}

func (s Service) GetPrefix() string {
	return utils.FindAnnotation(s.Metadata.Annotations, "tenant.service.alias.prefix")
}
