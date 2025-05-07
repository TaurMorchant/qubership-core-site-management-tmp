package domain

import (
	"fmt"
)

type (
	Configmap struct {
		Metadata ConfigMapMetaData `json:"metadata"`
		Data     ConfigMapData     `json:"data"`
	}

	ConfigMapMetaData struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}

	ConfigMapData struct {
		Parent         string `json:"baseline"`
		ExternalRoutes string `json:"common-external-routes.json"`
	}
)

func (c Configmap) String() string {
	return fmt.Sprintf("ConfigMap{metadata=%s,data=%s}", c.Metadata, c.Data)
}

func (d ConfigMapData) String() string {
	return fmt.Sprintf("Data{parent=%s,externalRoutes=%s}", d.Parent, d.ExternalRoutes)
}
