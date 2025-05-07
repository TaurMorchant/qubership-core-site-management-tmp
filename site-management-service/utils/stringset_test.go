package utils

import (
	"testing"
)

func TestStringSet_Api(t *testing.T) {
	actual := New("a", "B")

	if !actual.Contains("a") {
		t.Fatal()
	}

	if actual.Contains("TT") {
		t.Fatal()
	}

	if !actual.ContainsIgnoreCase("b") {
		t.Fatal()
	}

	actual.Put("c")
	if !actual.ContainsIgnoreCase("C") {
		t.Fatal()
	}
}

func TestStringSet_ToSlice(t *testing.T) {
	actual := make(Set)
	actual.Put("a", "b")

	arr := *actual.ToSlice()
	if len(arr) != 2 {
		t.Fatal()
	}

	if arr[0] != "a" && arr[1] != "a" {
		t.Fatal()
	}

	if arr[0] != "b" && arr[1] != "b" {
		t.Fatal()
	}
}
