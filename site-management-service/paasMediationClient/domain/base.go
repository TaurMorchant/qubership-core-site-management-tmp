package domain

import "fmt"

type Metadata struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Annotations map[string]string `json:"annotations"`
}

type CustomService struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

type CommonObject struct {
	Metadata Metadata `json:"metadata"`
}

func (m Metadata) String() string {
	return fmt.Sprintf("Metadata{name=%s,namespace=%s,annotations=%v}", m.Name, m.Namespace, m.Annotations)
}
