package handler

import (
	"strings"
	"testing"
)

func TestWriteSSEDataPreservesBareCarriageReturn(t *testing.T) {
	var builder strings.Builder

	// 终端进度条常用裸 \r 回到行首覆盖内容，这里必须保留下来，
	// 不能在 SSE 层直接洗成 \n，否则前端永远拿不到“单行覆盖刷新”的语义。
	writeSSEData(&builder, "1s/10s (10%) [=>]\r2s/10s (20%) [==>]\n完成")

	got := builder.String()
	if !strings.Contains(got, "\r") {
		t.Fatalf("expected SSE payload to keep bare carriage return, got %q", got)
	}
	if strings.Contains(got, "1s/10s (10%) [=>]\n2s/10s (20%) [==>]") {
		t.Fatalf("expected bare carriage return to avoid forced line split, got %q", got)
	}
}
