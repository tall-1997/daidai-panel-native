package mcptools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 订阅的增删改与启停（#139），对应 POST /subscriptions、PUT / DELETE /subscriptions/:id、PUT /subscriptions/:id/enable|disable。
func (t *toolset) registerSubscriptionWriteTools(s *mcp.Server) {
	addWriteTool(s, "create_subscription", "新建订阅",
		"新建一个订阅（Git 仓库或单文件），建好即启用：填了 schedule 会按它自动拉取，也可以随后用 pull_subscription 立即拉取一次。"+
			"拉取会把脚本同步进脚本目录，并按设置自动增删任务；pre_script / hook_script 会以面板的权限执行。",
		writeHints{}, t.createSubscription)
	addWriteTool(s, "update_subscription", "修改订阅",
		"按 ID 修改订阅设置，只改传入的字段，原值被覆盖。改动在下一次拉取时生效。",
		writeHints{destructive: true, idempotent: true}, t.updateSubscription)
	addWriteTool(s, "delete_subscription", "删除订阅",
		"按 ID 删除订阅及其拉取记录，不可恢复；已经同步到脚本目录的文件和已建的任务不会被删除。面板对不存在的 ID 也会回成功，请先用 list_subscriptions 确认 ID。",
		writeHints{destructive: true, idempotent: true}, t.deleteSubscription)
	addWriteTool(s, "set_subscription_enabled", "启用或禁用订阅",
		"打开或关闭订阅的启用开关：禁用后不再按 schedule 自动拉取（仍可手动拉取），已同步的脚本与任务不受影响。",
		writeHints{idempotent: true}, t.setSubscriptionEnabled)
}

// subscriptionFields 是新建与修改共用的可选字段，指针为 nil 表示没传。
// auth_token 只写不读：订阅接口从不回显令牌明文，输出里只有 has_auth_token。
type subscriptionFields struct {
	Type            *string `json:"type,omitempty" jsonschema:"类型：git-repo（Git 仓库，默认）或 single-file（单文件，url 指向脚本文件）"`
	Branch          *string `json:"branch,omitempty" jsonschema:"Git 分支，留空用仓库默认分支"`
	Schedule        *string `json:"schedule,omitempty" jsonschema:"自动拉取的定时规则（cron 表达式），留空表示不自动拉取"`
	Whitelist       *string `json:"whitelist,omitempty" jsonschema:"白名单：只同步匹配的文件，多条用 | 或英文逗号分隔，含括号等正则字符的片段按正则解析"`
	Blacklist       *string `json:"blacklist,omitempty" jsonschema:"黑名单：不同步匹配的文件，写法同白名单"`
	DependOn        *string `json:"depend_on,omitempty" jsonschema:"依赖文件：会一起同步但不建任务的文件，写法同白名单"`
	SubPath         *string `json:"sub_path,omitempty" jsonschema:"只同步仓库里的这个子目录"`
	SaveDir         *string `json:"save_dir,omitempty" jsonschema:"同步到脚本目录下的哪个子目录，默认取别名或仓库名"`
	Alias           *string `json:"alias,omitempty" jsonschema:"别名"`
	AutoAddTaskMode *string `json:"auto_add_task_mode,omitempty" jsonschema:"拉取时自动添加定时任务：inherit（跟随全局设置，默认）、enabled、disabled"`
	AutoDelTaskMode *string `json:"auto_del_task_mode,omitempty" jsonschema:"拉取时自动删除失效任务：inherit（跟随全局设置，默认）、enabled、disabled"`
	OverwriteMode   *string `json:"overwrite_mode,omitempty" jsonschema:"本地改动的处理：inherit（跟随全局设置，默认）、force（强制用远端覆盖本地改动）、preserve（保留本地改动）"`
	FullCheckout    *bool   `json:"full_checkout,omitempty" jsonschema:"为 true 时整仓检出，不按白名单做稀疏检出"`
	PreScript       *string `json:"pre_script,omitempty" jsonschema:"拉取前执行的 shell 命令"`
	HookScript      *string `json:"hook_script,omitempty" jsonschema:"拉取成功后执行的 shell 命令"`
	AuthType        *string `json:"auth_type,omitempty" jsonschema:"私有仓库鉴权方式：token 或 ssh，传空字符串表示不鉴权"`
	AuthUsername    *string `json:"auth_username,omitempty" jsonschema:"token 鉴权时的用户名（部分平台需要）"`
	AuthToken       *string `json:"auth_token,omitempty" jsonschema:"访问令牌，auth_type 为 token 时必填；面板不会回显"`
	SSHKeyID        *int64  `json:"ssh_key_id,omitempty" jsonschema:"SSH 密钥 ID，auth_type 为 ssh 时必填"`
}

type createSubscriptionInput struct {
	Name string `json:"name" jsonschema:"订阅名称"`
	URL  string `json:"url" jsonschema:"仓库地址或单文件地址"`
	subscriptionFields
}

type updateSubscriptionInput struct {
	ID   int64   `json:"id" jsonschema:"订阅 ID，可从 list_subscriptions 获得"`
	Name *string `json:"name,omitempty" jsonschema:"新的订阅名称"`
	URL  *string `json:"url,omitempty" jsonschema:"新的仓库地址或单文件地址"`
	subscriptionFields
}

type setSubscriptionEnabledInput struct {
	ID      int64 `json:"id" jsonschema:"订阅 ID"`
	Enabled bool  `json:"enabled" jsonschema:"true 启用，false 禁用"`
}

// 三态字段的取值与 model 里的定义一致。面板对脏值是静默归成 inherit 的，
// 这里提前拦下，AI 拼错时能拿到明确的错误而不是「保存成功但没生效」。
var (
	subscriptionTypeValues     = map[string]bool{"git-repo": true, "single-file": true}
	subscriptionTaskSyncValues = map[string]bool{"inherit": true, "enabled": true, "disabled": true}
	subscriptionOverwriteModes = map[string]bool{"inherit": true, "force": true, "preserve": true}
)

func (f subscriptionFields) addTo(body map[string]any) error {
	enums := []struct {
		key     string
		value   *string
		allowed map[string]bool
		hint    string
	}{
		{"type", f.Type, subscriptionTypeValues, "git-repo、single-file"},
		{"auto_add_task_mode", f.AutoAddTaskMode, subscriptionTaskSyncValues, "inherit、enabled、disabled"},
		{"auto_del_task_mode", f.AutoDelTaskMode, subscriptionTaskSyncValues, "inherit、enabled、disabled"},
		{"overwrite_mode", f.OverwriteMode, subscriptionOverwriteModes, "inherit、force、preserve"},
	}
	for _, enum := range enums {
		if enum.value == nil {
			continue
		}
		value := strings.ToLower(strings.TrimSpace(*enum.value))
		if !enum.allowed[value] {
			return fmt.Errorf("%s 只能是 %s，收到 %q", enum.key, enum.hint, *enum.value)
		}
		body[enum.key] = value
	}
	if f.AuthType != nil {
		authType := strings.ToLower(strings.TrimSpace(*f.AuthType))
		if authType != "" && authType != "token" && authType != "ssh" {
			return fmt.Errorf("auth_type 只能是 token、ssh 或空字符串，收到 %q", *f.AuthType)
		}
		body["auth_type"] = authType
	}
	setIfPresent(body, "branch", f.Branch)
	setIfPresent(body, "schedule", f.Schedule)
	setIfPresent(body, "whitelist", f.Whitelist)
	setIfPresent(body, "blacklist", f.Blacklist)
	setIfPresent(body, "depend_on", f.DependOn)
	setIfPresent(body, "sub_path", f.SubPath)
	setIfPresent(body, "save_dir", f.SaveDir)
	setIfPresent(body, "alias", f.Alias)
	setIfPresent(body, "full_checkout", f.FullCheckout)
	setIfPresent(body, "pre_script", f.PreScript)
	setIfPresent(body, "hook_script", f.HookScript)
	setIfPresent(body, "auth_username", f.AuthUsername)
	setIfPresent(body, "auth_token", f.AuthToken)
	setIfPresent(body, "ssh_key_id", f.SSHKeyID)
	return nil
}

func (t *toolset) createSubscription(ctx context.Context, in createSubscriptionInput) (any, error) {
	name := strings.TrimSpace(in.Name)
	address := strings.TrimSpace(in.URL)
	if name == "" || address == "" {
		return nil, errors.New("name 与 url 都不能为空")
	}
	body := map[string]any{"name": name, "url": address}
	if err := in.subscriptionFields.addTo(body); err != nil {
		return nil, err
	}
	result, err := t.call(ctx, http.MethodPost, "/subscriptions", nil, body)
	if err != nil {
		return nil, err
	}
	return subscriptionMutationOutput(result), nil
}

func (t *toolset) updateSubscription(ctx context.Context, in updateSubscriptionInput) (any, error) {
	if err := requireID("id", in.ID); err != nil {
		return nil, err
	}
	body := map[string]any{}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, errors.New("name 不能改成空")
		}
		body["name"] = name
	}
	if in.URL != nil {
		address := strings.TrimSpace(*in.URL)
		if address == "" {
			return nil, errors.New("url 不能改成空")
		}
		body["url"] = address
	}
	if err := in.subscriptionFields.addTo(body); err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, errors.New("没有要修改的字段：至少传一个 id 以外的参数")
	}
	result, err := t.call(ctx, http.MethodPut, fmt.Sprintf("/subscriptions/%d", in.ID), nil, body)
	if err != nil {
		return nil, err
	}
	return subscriptionMutationOutput(result), nil
}

func (t *toolset) deleteSubscription(ctx context.Context, in subscriptionIDInput) (any, error) {
	if err := requireID("id", in.ID); err != nil {
		return nil, err
	}
	result, err := t.call(ctx, http.MethodDelete, fmt.Sprintf("/subscriptions/%d", in.ID), nil, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{"message": result["message"], "subscription_id": in.ID}, nil
}

func (t *toolset) setSubscriptionEnabled(ctx context.Context, in setSubscriptionEnabledInput) (any, error) {
	if err := requireID("id", in.ID); err != nil {
		return nil, err
	}
	action := "disable"
	if in.Enabled {
		action = "enable"
	}
	result, err := t.call(ctx, http.MethodPut, fmt.Sprintf("/subscriptions/%d/%s", in.ID, action), nil, nil)
	if err != nil {
		return nil, err
	}
	return subscriptionMutationOutput(result), nil
}

// subscriptionMutationOutput 在 list_subscriptions 的精简字段之外，再带上写入时能改的几项，
// 调用方能直接核对刚才改的值有没有生效。接口本身不下发 auth_token。
func subscriptionMutationOutput(result map[string]any) map[string]any {
	out := map[string]any{"message": result["message"]}
	if sub, ok := result["data"].(map[string]any); ok {
		out["subscription"] = pickFields(sub, "id", "name", "alias", "type", "url", "branch", "schedule",
			"whitelist", "blacklist", "depend_on", "sub_path", "save_dir", "enabled", "status", "last_pull_at",
			"auth_type", "auth_username", "has_auth_token", "ssh_key_id", "auto_add_task_mode", "auto_del_task_mode",
			"overwrite_mode", "full_checkout", "pre_script", "hook_script")
	}
	return out
}
