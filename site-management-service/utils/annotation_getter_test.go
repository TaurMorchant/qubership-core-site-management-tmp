package utils

import (
	"reflect"
	"testing"
)

func TestGroupAnnotationGetter_Get(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		groups      []string
		key         string
		expectedVal string
		expectedOk  bool
	}{
		{
			name: "key exists in first group",
			annotations: map[string]string{
				"group1/key1": "value1",
				"group2/key1": "value2",
			},
			groups:      []string{"group1", "group2"},
			key:         "key1",
			expectedVal: "value1",
			expectedOk:  true,
		},
		{
			name: "key exists in second group",
			annotations: map[string]string{
				"group1/key1": "value1",
				"group2/key1": "value2",
			},
			groups:      []string{"group2", "group1"},
			key:         "key1",
			expectedVal: "value2",
			expectedOk:  true,
		},
		{
			name: "key does not exist in any group",
			annotations: map[string]string{
				"group1/key1": "value1",
				"group2/key1": "value2",
			},
			groups:      []string{"group1", "group2"},
			key:         "key2",
			expectedVal: "",
			expectedOk:  false,
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			groups:      []string{"group1", "group2"},
			key:         "key1",
			expectedVal: "",
			expectedOk:  false,
		},
		{
			name: "empty groups",
			annotations: map[string]string{
				"group1/key1": "value1",
			},
			groups:      []string{},
			key:         "key1",
			expectedVal: "",
			expectedOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getter := NewBaseAnnotationMapper(tt.groups...)
			val, ok := getter.Get(tt.annotations, tt.key)

			if val != tt.expectedVal {
				t.Errorf("expected value %q, got %q", tt.expectedVal, val)
			}

			if ok != tt.expectedOk {
				t.Errorf("expected ok %v, got %v", tt.expectedOk, ok)
			}
		})
	}
}

func TestGroupAnnotationMapper_Set(t *testing.T) {
	tests := []struct {
		name            string
		initAnnotations map[string]string
		expAnnotations  map[string]string
		groups          []string
	}{
		{
			name: "key exists in first group",
			initAnnotations: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			expAnnotations: map[string]string{
				"group1/key1": "value1",
				"group2/key1": "value1",
				"group1/key2": "value2",
				"group2/key2": "value2",
			},
			groups: []string{"group1", "group2"},
		},
		{
			name:            "empty annotations",
			initAnnotations: map[string]string{},
			expAnnotations:  map[string]string{},
			groups:          []string{"group1", "group2"},
		},
		{
			name: "empty groups",
			initAnnotations: map[string]string{
				"key1": "value1",
			},
			expAnnotations: map[string]string{},
			groups:         []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getter := NewBaseAnnotationMapper(tt.groups...)
			expAnn := getter.Set(tt.initAnnotations)
			if !reflect.DeepEqual(expAnn, tt.expAnnotations) {
				t.Errorf("expected annotations %v, got %v", tt.expAnnotations, expAnn)
			}

		})
	}
}
