package model_test

import (
	"strings"
	"testing"

	"daidai-panel/model"
)

// MCP 两个开关（issue #128，契约 C11）：bool、默认关闭、归入 mcp 分组「MCP 服务」。
func TestMCPConfigsAreRegisteredAndDisabledByDefault(t *testing.T) {
	for _, key := range []string{model.MCPEnabledConfigKey, model.MCPAllowMutationsConfigKey} {
		def, ok := model.GetSystemConfigDefinition(key)
		if !ok {
			t.Fatalf("%s 应当是已注册配置", key)
		}
		if def.ValueType != model.SystemConfigTypeBool || def.DefaultValue != "false" {
			t.Errorf("%s 应当是默认 false 的布尔项，实际 type=%s default=%q", key, def.ValueType, def.DefaultValue)
		}
		if def.Group != "mcp" || def.GroupLabel != "MCP 服务" {
			t.Errorf("%s 应当归入 mcp 分组「MCP 服务」，实际 %s / %s", key, def.Group, def.GroupLabel)
		}
		// 这两项对网页、APP、所有 MCP 客户端都生效，不能照抄 panel_shape_style 的「仅网页端生效」。
		if strings.Contains(def.Description, "仅网页端生效") {
			t.Errorf("%s 的说明不应写「仅网页端生效」", key)
		}
	}
	if model.MCPEnabledConfigKey != "mcp_enabled" || model.MCPAllowMutationsConfigKey != "mcp_allow_mutations" {
		t.Fatal("配置键名是跨组契约 C11，不能改名")
	}
}
