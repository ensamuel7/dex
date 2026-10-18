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

func TestLintOnByDefault(t *testing.T) {
	reset := func() { lintAsked, noLint = false, false }
	cases := []struct {
		asked, off bool
		command    string
		want       bool
	}{
		{false, false, "build", true},
		{false, false, "run", true},
		{false, false, "dev", true},
		{false, false, "test", false},
		{true, false, "test", true},
		{false, true, "build", false},
	}
	for _, c := range cases {
		reset()
		lintAsked, noLint = c.asked, c.off
		if got := lintOnByDefault(c.command); got != c.want {
			t.Errorf("%s with --lint=%v --no-lint=%v: got %v, want %v", c.command, c.asked, c.off, got, c.want)
		}
	}
	reset()
}
