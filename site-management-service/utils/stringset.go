package utils

import "strings"

type Set map[string]int

func New(values ...string) *Set {
	result := make(Set)
	for _, v := range values {
		result.Put(v)
	}
	return &result
}

func (c *Set) Put(values ...string) *Set {
	for _, v := range values {
		(*c)[v] = 0
	}
	return c
}

func (c *Set) Contains(value string) bool {
	_, ok := (*c)[value]
	return ok
}

func (c *Set) ContainsIgnoreCase(value string) bool {
	lowerValue := strings.ToLower(value)
	for key := range *c {
		if strings.ToLower(key) == lowerValue {
			return true
		}
	}
	return false
}

func (c *Set) ToSlice() *[]string {
	result := make([]string, len(*c))
	i := 0
	for k := range *c {
		result[i] = k
		i++
	}
	return &result
}
