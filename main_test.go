package main

import (
	"reflect"
	"testing"
)

func TestKeepLastOccurrence(t *testing.T) {
	got := keepLastOccurrence([]string{"-lssl", "-lcrypto", "-lz", "-L/opt/lib", "-lssl", "-lcrypto"})
	want := []string{"-lz", "-L/opt/lib", "-lssl", "-lcrypto"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := keepLastOccurrence(nil); len(got) != 0 {
		t.Errorf("got %v for nil", got)
	}
}
