package utils

type AnnotationGetter interface {
	Get(annotations map[string]string, key string) (string, bool)
}

type GroupAnnotationGetter struct {
	groups []string
}

func NewBaseAnnotationGetter(groups ...string) *GroupAnnotationGetter {
	return &GroupAnnotationGetter{groups: groups}
}

func (g GroupAnnotationGetter) Get(annotations map[string]string, key string) (string, bool) {
	for _, v := range g.groups {
		if value, found := annotations[v+"/"+key]; found {
			return value, true
		}
	}
	return "", false
}
