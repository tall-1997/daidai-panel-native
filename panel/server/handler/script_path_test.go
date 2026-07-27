package handler

import "testing"

func TestNormalizeScriptRelativePath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "simple root file", input: "demo.sh", want: "demo.sh"},
		{name: "normalize repeated separators", input: " folder//sub/./demo.py ", want: "folder/sub/demo.py"},
		{name: "normalize windows separators", input: `folder\child\demo.js`, want: "folder/child/demo.js"},
		{name: "reject traversal", input: "../outside", wantErr: "不允许路径穿越"},
		{name: "reject nested traversal", input: "folder/../outside", wantErr: "不允许路径穿越"},
		{name: "reject absolute-like path", input: "/outside/demo.sh", wantErr: "不允许路径穿越"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeScriptRelativePath(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
