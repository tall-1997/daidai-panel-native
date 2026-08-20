package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// NormalizeNotifyChannelConfig 把通知渠道的 config 归一成服务端唯一能消费的形态：
// 顶层是 JSON 对象，且每个值都是字符串。
//
// 为什么必须有这个函数：
// service.sendToChannel 里是 `var cfg map[string]string; json.Unmarshal(...)`。
// 只要 config 里出现一个非字符串值，Unmarshal 就返回 UnmarshalTypeError，
// 于是这个渠道的**所有**通知（包括「测试」按钮）从此全部失败，错误信息还是
// 一句用户完全看不懂的 `invalid config: json: cannot unmarshal bool into Go
// value of type string`。这个坑真实发生过：APP 把 smtp_ssl 写成了 JSON 布尔
// false，整个邮件渠道直接哑掉，而 Web 端读回来原样写回去，连修都修不好。
//
// 归一规则，以及为什么这么分：
//
//   - 字符串              -> 原样保留。
//   - 布尔 / 数字 / null   -> 转成字符串。这几种是**安全可逆**的：服务端消费侧
//     本来就是 notificationConfigBool / notificationConfigInt 这类字符串解析器，
//     "false" 和 false 表达的是同一个意思，转换不丢信息。
//     这条同时让老客户端写坏的记录「一编辑就自愈」，不需要额外做数据迁移。
//   - 对象 / 数组          -> 直接报错。这类**不可逆**：fmt.Sprint 出来是
//     `map[Authorization:Bearer xxx]` 这种 Go 语法垃圾，没有任何 cfg[...] 的消费者
//     能解析它，静默存下去等于把「发不出去」换成「发出去的是垃圾」。
//     而且这恰恰是 custom 渠道最容易踩的坑：headers / body 的值本身是 JSON 文本，
//     但在 config 里必须是**字符串**，用户在原始 JSON 编辑框里写成嵌套对象就会命中这条。
//
// 数字用 json.Decoder + UseNumber 解析，不能用默认的 interface{} 反序列化：
// 默认会得到 float64，fmt.Sprint(float64(1000000)) 是 "1e+06"，直接把用户填的
// 超时时间之类的整数毁掉。
//
// 注意：返回值的键顺序由 encoding/json 按字典序重排，与输入顺序无关。
// JSON 对象本来就无序，客户端都是按 schema 顺序渲染表单的，不受影响。
func NormalizeNotifyChannelConfig(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "{}", nil
	}

	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()

	var decoded interface{}
	if err := decoder.Decode(&decoded); err != nil {
		return "", fmt.Errorf("通知渠道配置不是合法的 JSON: %s", err.Error())
	}
	// Decode 只消费第一个 JSON 值，后面还有内容说明整体是坏的（例如 `{} {}`）。
	if decoder.More() {
		return "", fmt.Errorf("通知渠道配置不是合法的 JSON: 结尾存在多余内容")
	}

	object, ok := decoded.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf(`通知渠道配置必须是 JSON 对象，例如 {"url":"https://example.com/webhook"}`)
	}

	normalized := make(map[string]string, len(object))
	for key, value := range object {
		text, reject := notifyChannelConfigValueToString(value)
		if reject != "" {
			return "", fmt.Errorf("通知渠道配置项 %q %s", key, reject)
		}
		normalized[key] = text
	}

	// 用 Encoder 并关掉 HTML 转义。默认的 json.Marshal 会把 URL query 里常见的
	// 尖括号和与号转成 Unicode 转义序列，用户下次打开原始 JSON 编辑框会看到一串
	// 不认识的 \u00xx，误以为配置被改坏了。
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(normalized); err != nil {
		return "", fmt.Errorf("通知渠道配置序列化失败: %s", err.Error())
	}

	return strings.TrimRight(buf.String(), "\n"), nil
}

// notifyChannelConfigValueToString 归一单个值。
// 第二个返回值是拒绝原因（中文句子片段，供上层拼上键名）；为空串表示归一成功。
func notifyChannelConfigValueToString(value interface{}) (string, string) {
	switch typed := value.(type) {
	case string:
		return typed, ""
	case json.Number:
		return typed.String(), ""
	case bool:
		if typed {
			return "true", ""
		}
		return "false", ""
	case nil:
		// null 视为「没填」，落成空串，与前端清空输入框的效果一致。
		return "", ""
	case map[string]interface{}:
		return "", "的值必须是字符串，当前是 JSON 对象。需要传 JSON 内容时请把它整体转成字符串再填写"
	case []interface{}:
		return "", "的值必须是字符串，当前是 JSON 数组。需要传 JSON 内容时请把它整体转成字符串再填写"
	default:
		return "", "的值必须是字符串，当前类型无法识别"
	}
}
