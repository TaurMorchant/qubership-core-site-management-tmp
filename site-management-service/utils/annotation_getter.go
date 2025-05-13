package utils

type AnnotationMapper interface {
	Get(annotations map[string]string, key string) (string, bool)
	Set(annotations map[string]string) map[string]string
}

type GroupAnnotationMapper struct {
	groups []string
}

func NewBaseAnnotationMapper(groups ...string) *GroupAnnotationMapper {
	return &GroupAnnotationMapper{groups: groups}
}

func (g GroupAnnotationMapper) Get(annotations map[string]string, key string) (string, bool) {
	for _, v := range g.groups {
		if value, found := annotations[v+"/"+key]; found {
			return value, true
		}
	}
	return "", false
}

func (g GroupAnnotationMapper) Set(annotations map[string]string) map[string]string {
	labeledAnnotations := make(map[string]string)
	for _, group := range g.groups {
		for k, v := range annotations {
			labeledAnnotations[group+"/"+k] = v
		}
	}
	return labeledAnnotations
}
