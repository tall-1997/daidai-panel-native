package handler

import (
	"strings"
	"testing"
)

// 【裸 \r 的 SSE 契约（本轮更新，改之前先读完这段）】
//
// 老契约只说了一半：「writeSSEData 不能把裸 \r 洗成 \n」。这条仍然成立 ——
// 终端进度条靠裸 \r 回到行首覆盖当前行，抹掉它日志就会刷屏。
//
// 但只保留字符是不够的：SSE 线格式里 CR 本身就是行终止符。
// `data: 10%\r20%` 在标准解析器（浏览器 EventSource、独立 APP）眼里是两行，
// 第二行 `20%` 没有字段名会被静默丢弃 —— 打开日志弹窗时喂进去的是整份历史日志，
// 带进度条的日志等于只剩第一个 \r 之前那一点。
//
// 所以新契约是两条：
//  1. \r 字符必须原样出现在输出里（不许换成 \n，也不许删）；
//  2. 每个裸 \r 后面必须紧跟 \n（即它落在一条 data 行的【末尾】），
//     后续内容另起一帧继续写，任何解析器都不会丢内容。
//
// 同时保证「切分不增删字符」：把所有帧的 data 值按顺序接起来，必须等于原始输入
// （\r\n 归一成 \n 除外）。前端就是这么拼的，所以这条等价性一破，日志内容就变了。
func TestWriteSSEDataPreservesBareCarriageReturn(t *testing.T) {
	var builder strings.Builder

	input := "1s/10s (10%) [=>]\r2s/10s (20%) [==>]\n完成"
	writeSSEData(&builder, input)

	got := builder.String()
	if !strings.Contains(got, "\r") {
		t.Fatalf("expected SSE payload to keep bare carriage return, got %q", got)
	}
	if strings.Contains(got, "1s/10s (10%) [=>]\n2s/10s (20%) [==>]") {
		t.Fatalf("expected bare carriage return to avoid forced line split, got %q", got)
	}

	// 契约 2：每个 \r 后面都得是 \n，不能再有内容跟在同一条 data 行里。
	for i := 0; i < len(got); i++ {
		if got[i] != '\r' {
			continue
		}
		if i+1 >= len(got) || got[i+1] != '\n' {
			t.Fatalf("expected every bare CR to end its data line, got %q", got)
		}
	}

	// 契约 3：只切分、不增删字符。这里按 web/src/utils/sse.ts 的口径把帧拼回去。
	if rebuilt := rebuildSSEPayload(got); rebuilt != input {
		t.Fatalf("expected frames to rebuild original payload %q, got %q", input, rebuilt)
	}
}

// 多个连续裸 \r、以及 \r 恰好落在整段末尾这两种边界，都不能把内容切丢或切重。
func TestWriteSSEDataFramesConsecutiveCarriageReturns(t *testing.T) {
	for _, input := range []string{
		"a\r\rb",
		"a\r",
		"\rb",
		"\r",
		"a\r\nb\rc\nd",
		"",
	} {
		var builder strings.Builder
		writeSSEData(&builder, input)

		want := strings.ReplaceAll(input, "\r\n", "\n")
		if got := rebuildSSEPayload(builder.String()); got != want {
			t.Fatalf("input %q: expected rebuilt payload %q, got %q (wire=%q)", input, want, got, builder.String())
		}
	}
}

// rebuildSSEPayload 复刻前端 web/src/utils/sse.ts 的解析：
// 按空行切帧，帧内取 data 字段（去掉字段名后的一个空格）并用 \n 拼接，
// 帧与帧之间【不补任何字符】—— LogViewer 就是把相邻消息首尾相接 append 的。
func rebuildSSEPayload(wire string) string {
	var out strings.Builder
	for _, frame := range strings.Split(wire, "\n\n") {
		if frame == "" {
			continue
		}
		lines := make([]string, 0, 4)
		for _, line := range strings.Split(frame, "\n") {
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			value := line[len("data:"):]
			if strings.HasPrefix(value, " ") {
				value = value[1:]
			}
			lines = append(lines, value)
		}
		out.WriteString(strings.Join(lines, "\n"))
	}
	return out.String()
}
