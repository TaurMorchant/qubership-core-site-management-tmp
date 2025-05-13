package utils

import (
	"github.com/netcracker/qubership-core-lib-go/v3/serviceloader"
	"github.com/netcracker/qubership-core-lib-go/v3/utils"
)

func FindAnnotation(haystack map[string]string, needle string) string {
	if value, ok := serviceloader.MustLoad[utils.AnnotationMapper]().Find(haystack, needle); ok {
		return value
	} else {
		return ""
	}
}
