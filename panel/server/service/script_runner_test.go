package service

import (
	"fmt"
	"testing"
)

func TestIsBenignProcessPipeReadError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: true},
		{name: "closed file", err: fmt.Errorf("read |0: file already closed"), want: true},
		{name: "closed pipe", err: fmt.Errorf("io: read/write on closed pipe"), want: true},
		{name: "real error", err: fmt.Errorf("permission denied"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBenignProcessPipeReadError(tt.err); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}
