package domain

import (
	"github.com/netcracker/qubership-core-lib-go/v3/serviceloader"
	utilsCore "github.com/netcracker/qubership-core-lib-go/v3/utils"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/utils"
	"reflect"
	"strconv"
	"strings"
)

type Route struct {
	Metadata Metadata  `json:"metadata"`
	Spec     RouteSpec `json:"spec"`
}

type RouteSpec struct {
	Host    string    `json:"host"`
	Path    string    `json:"path"`
	Service Target    `json:"to"`
	Port    RoutePort `json:"port"`
}

type Target struct {
	Name string `json:"name"`
}

type RoutePort struct {
	TargetPort int32 `json:"targetPort"`
}

func (r Route) GetPriority() int {
	mapper := serviceloader.MustLoad[utilsCore.AnnotationMapper]()
	if value, ok := mapper.Find(r.Metadata.Annotations, "tenant.service.tenant.id"); ok && value == "GENERAL" {
		return -1
	} else {
		if value, ok := mapper.Find(r.Metadata.Annotations, "tenant.service.order"); ok {
			if result, err := strconv.Atoi(value); err != nil {
				return result
			}
		}
		return 0
	}
}

func (r Route) GetServiceDescription() string {
	return utils.FindAnnotation(r.Metadata.Annotations, "tenant.service.show.description")
}

func (r Route) GetServiceName() string {
	return utils.FindAnnotation(r.Metadata.Annotations, "tenant.service.show.name")
}

func (r Route) GetServiceSuffix() string {
	return utils.FindAnnotation(r.Metadata.Annotations, "tenant.service.url.suffix")
}

func (r Route) GetServiceId(defaultValue string) string {
	if value, ok := serviceloader.MustLoad[utilsCore.AnnotationMapper]().Find(r.Metadata.Annotations, "tenant.service.id"); ok {
		return value
	} else {
		return defaultValue
	}
}

func (r Route) GetTenantId() string {
	return utils.FindAnnotation(r.Metadata.Annotations, "tenant.service.tenant.id")
}

func (r Route) String() string {
	return r.FormatString("")
}

func (r Route) FormatString(leftAlignPrefix string) string {
	b := strings.Builder{}
	b.WriteString(leftAlignPrefix)
	b.WriteString("Metadata:")
	b.WriteString("\n")
	b.WriteString(leftAlignPrefix)
	b.WriteString("\tName: ")
	b.WriteString(r.Metadata.Name)
	b.WriteString("\n")
	b.WriteString(leftAlignPrefix)
	b.WriteString("\tAnnotations: ")
	for k, v := range r.Metadata.Annotations {
		b.WriteString("\n")
		b.WriteString(leftAlignPrefix)
		b.WriteString("\t\t")
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
	}
	b.WriteString("\n")
	b.WriteString(leftAlignPrefix)
	b.WriteString("Spec:")
	b.WriteString("\n")
	b.WriteString(leftAlignPrefix)
	b.WriteString("\tHost: ")
	b.WriteString(r.Spec.Host)
	b.WriteString("\n")
	b.WriteString(leftAlignPrefix)
	b.WriteString("\tService: ")
	b.WriteString("\n")
	b.WriteString(leftAlignPrefix)
	b.WriteString("\t\tName: ")
	b.WriteString(r.Spec.Service.Name)

	return b.String()
}

func (r *Route) MergeRoute(route *Route) {
	if !reflect.DeepEqual(r.Spec.Port, route.Spec.Port) {
		r.Spec.Port = route.Spec.Port
	}
}
