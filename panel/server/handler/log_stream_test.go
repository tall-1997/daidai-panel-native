package handler

import (
	"bytes"
	"testing"
)

func TestWriteSSEDataPrefixesEveryLine(t *testing.T) {
	var buf bytes.Buffer
	writeSSEData(&buf, "first\nsecond\r\nthird\n")

	want := "data: first\ndata: second\ndata: third\ndata: \n\n"
	if got := buf.String(); got != want {
		t.Fatalf("expected SSE data %q, got %q", want, got)
	}
}
