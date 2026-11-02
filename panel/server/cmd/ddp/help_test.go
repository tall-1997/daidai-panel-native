package main

import (
	"strings"
	"testing"
)

// 总帮助里 ddp mcp 的用法与示例必须是真实可用的参数：逐行交给 ddp mcp 自己的 parseMCPArgs 解析。
// 以后 mcp.go 改了参数名、总帮助没跟上，这里直接报出来。「说明」段是散文，不参与解析。
func TestHelpDocumentsMCPCommandWithRealFlags(t *testing.T) {
	section := ""
	var usageLines, exampleLines []string
	for _, line := range strings.Split(helpText, "\n") {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case "用法:", "说明:", "示例:":
			section = trimmed
			continue
		}
		if !strings.Contains(trimmed, "ddp mcp") {
			continue
		}
		switch section {
		case "用法:":
			usageLines = append(usageLines, trimmed)
		case "示例:":
			exampleLines = append(exampleLines, trimmed)
		}
	}

	if len(usageLines) != 1 || !strings.HasPrefix(usageLines[0], "ddp mcp ") {
		t.Fatalf("「用法」里应有且只有一行 ddp mcp，实际 %q", usageLines)
	}

	sawURL, sawDockerExec := false, false
	for _, line := range append(append([]string{}, usageLines...), exampleLines...) {
		if strings.HasPrefix(line, "docker exec -i ") {
			sawDockerExec = true
		}
		rest := line[strings.Index(line, "ddp mcp")+len("ddp mcp"):]
		args := strings.Fields(strings.NewReplacer("[", " ", "]", " ").Replace(rest))
		opts, err := parseMCPArgs(args, func(string) string { return "" })
		if err != nil {
			t.Fatalf("帮助里的 %q 用了 ddp mcp 不认的参数：%v", line, err)
		}
		if opts.appKey == "" || opts.appSecret == "" {
			t.Fatalf("帮助里的 %q 应带上 --app-key 与 --app-secret", line)
		}
		if opts.url != "" {
			sawURL = true
		}
	}
	if !sawURL {
		t.Fatal("帮助里应有一处演示 --url")
	}
	if !sawDockerExec {
		t.Fatal("「示例」里应有 docker exec -i <容器名> ddp mcp 的写法（-i 不能少，否则 stdin 接不上）")
	}
}
