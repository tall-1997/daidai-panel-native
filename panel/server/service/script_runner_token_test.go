package service

import (
	"reflect"
	"testing"
)

func TestSplitCommandTokensPreservesExplicitEmptyArguments(t *testing.T) {
	got, err := splitCommandTokens(`task script.py -- "" '' value`)
	if err != nil {
		t.Fatalf("split command: %v", err)
	}
	want := []string{"task", "script.py", "--", "", "", "value"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens = %#v, want %#v", got, want)
	}
}
