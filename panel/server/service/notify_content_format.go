package service

import (
	"bytes"
	"mime/quotedprintable"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// 通知正文格式（issue #135）。
//
// 调用方（脚本 helper、POST /notifications/send）可以声明正文是 text / markdown / html，
// 由 notifier.go 的各 sendXxx 映射成渠道自己的格式开关。**空串不等于 text**：空串表示「调用方没声明」，
// 一切按渠道配置走、报文与加这个能力之前逐字节一致；写成 text 会把钉钉默认的 markdown 改成文本。
//
// 这个文件刻意不读 cfg["..."]、也不定义 send 开头的函数：notifier_schema_binding_test.go 只扫描 notifier.go，
// 放在这里的配置读取和发送函数会静默逃过「注册表 ↔ notifier」双向绑定。
const (
	NotifyContentText     = "text"
	NotifyContentMarkdown = "markdown"
	NotifyContentHTML     = "html"
)

// NormalizeNotifyContentType 归一调用方传入的正文格式：大小写与首尾空白不敏感，plain / txt 视为 text，md 视为 markdown，
// 也认 text/plain、text/markdown、text/html 这类 MIME 写法（分号后的 charset 等参数忽略）。
// 空串原样返回空串（老行为）；其余取值返回 ok=false，由调用方报 400。
func NormalizeNotifyContentType(raw string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "", true
	}
	if index := strings.IndexByte(value, ';'); index >= 0 {
		value = strings.TrimSpace(value[:index])
	}
	switch value {
	case "text", "plain", "txt", "text/plain":
		return NotifyContentText, true
	case "markdown", "md", "text/markdown":
		return NotifyContentMarkdown, true
	case "html", "text/html":
		return NotifyContentHTML, true
	default:
		return "", false
	}
}

// notifyChannelAcceptsHTML 声明每个渠道类型能否原样接收 HTML 正文，必须覆盖注册表里的全部类型
// （TestNotifyChannelHTMLCapabilityCoversRegistry 兜底：加渠道时漏了这里会直接红，逼着作者想清楚）。
//
//   - true：email 发 text/html、wxpusher 走 contentType=2、pushplus 走 html 模板；
//     webhook / custom 是用户自己的服务，原样透传，由接收方决定怎么渲染。
//   - false：其余渠道去掉标签转成纯文本再发。
//
// 不因为某些渠道「新版本支持 markdown / HTML 开关」就改成 true：ntfy 的 Markdown 头、gotify 的 extras、
// Bark 的 markdown 字段都依赖服务端版本，老版本静默忽略（issue #108 的教训），HTML 标签会原样显示出来。
var notifyChannelAcceptsHTML = map[string]bool{
	"webhook":    true,
	"email":      true,
	"telegram":   false,
	"dingtalk":   false,
	"wecom":      false,
	"wecom_app":  false,
	"bark":       false,
	"pushplus":   true,
	"serverchan": false,
	"feishu":     false,
	"gotify":     false,
	"pushdeer":   false,
	"pushme":     false,
	"chanify":    false,
	"igot":       false,
	"qmsg":       false,
	"pushover":   false,
	"discord":    false,
	"slack":      false,
	"ntfy":       false,
	"wxpusher":   true,
	"custom":     true,
	// 本机版（Android）渠道：应用内通知渲染富文本，外发 Webhook 由接收方决定，二者都原样透传 HTML。
	"android_local":    true,
	"external_webhook": true,
}

// adaptNotifyContent 在进入渠道分发之前按渠道能力改写正文，返回改写后的正文和交给 sendXxx 的格式。
//
// 渠道不认 HTML 时去标签转纯文本，格式交还成空串：钉钉 / 企业微信这类渠道「按渠道配置的文本类消息发」，
// 不因为调用方写了 html 就被强制改成 text 消息。text / markdown 与空串原样透传。
func adaptNotifyContent(channelType, contentType, content string) (string, string) {
	if contentType != NotifyContentHTML || notifyChannelAcceptsHTML[channelType] {
		return content, contentType
	}
	return notifyHTMLToText(content), ""
}

// encodeNotifyQuotedPrintable 给 HTML 邮件正文做 quoted-printable 编码：
// 压缩过的 HTML 常常一整段没有换行，单行超过 998 字节会被部分 SMTP 服务器拒收。
func encodeNotifyQuotedPrintable(content string) string {
	var buf bytes.Buffer
	writer := quotedprintable.NewWriter(&buf)
	_, _ = writer.Write([]byte(content))
	_ = writer.Close()
	return buf.String()
}

// notifyHTMLBlockAtoms 块级标签：开始与结束处各断一次行（已经在行首就不重复断）。
var notifyHTMLBlockAtoms = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Li: true, atom.Ul: true, atom.Ol: true,
	atom.Tr: true, atom.Table: true, atom.Thead: true, atom.Tbody: true, atom.Tfoot: true, atom.Caption: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Blockquote: true, atom.Pre: true, atom.Hr: true, atom.Section: true, atom.Article: true,
	atom.Header: true, atom.Footer: true, atom.Dl: true, atom.Dt: true, atom.Dd: true,
}

// notifyHTMLSkippedAtoms 内容整个丢弃的 HTML 元素：脚本、样式、页面标题不是给人看的正文；
// iframe / noembed / noframes / noscript 里是回退文字，浏览器（开着脚本）显示的是 src 页面或脚本结果，不显示它们；template 的内容也不显示。
//
// 除 template 外都是 tokenizer 按原始文本读到对应结束标签为止的元素（见 x/net/html 的 readStartTag）；
// template 不能省略结束标签，没闭合时浏览器同样把后文都当模板内容藏起来。
// 不能放 head：HTML 允许省略 </head>，tokenizer 不会补结束标签，按 head 计深度会把后面的正文整段吞掉。
// head 里真正有文字的只有 title / style / script，它们各自在这里跳过。
var notifyHTMLSkippedAtoms = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Title: true, atom.Noscript: true,
	atom.Iframe: true, atom.Noembed: true, atom.Noframes: true, atom.Template: true,
}

// notifyHTMLForeignBreakoutAtoms 在 SVG / MathML 里遇到就说明外来元素没写闭合：浏览器跳出外来内容，按 HTML 重新处理这个标签
// （HTML 标准「外来内容里的开始标签」一节，与 x/net/html 的 breakout 表一致；font 带属性的情况不处理）。
var notifyHTMLForeignBreakoutAtoms = map[atom.Atom]bool{
	atom.B: true, atom.Big: true, atom.Blockquote: true, atom.Body: true, atom.Br: true, atom.Center: true,
	atom.Code: true, atom.Dd: true, atom.Div: true, atom.Dl: true, atom.Dt: true, atom.Em: true, atom.Embed: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true, atom.Head: true,
	atom.Hr: true, atom.I: true, atom.Img: true, atom.Li: true, atom.Listing: true, atom.Menu: true, atom.Meta: true,
	atom.Nobr: true, atom.Ol: true, atom.P: true, atom.Pre: true, atom.Ruby: true, atom.S: true, atom.Small: true,
	atom.Span: true, atom.Strong: true, atom.Strike: true, atom.Sub: true, atom.Sup: true, atom.Table: true,
	atom.Tt: true, atom.U: true, atom.Ul: true, atom.Var: true,
}

// notifyForeignElement 是一个打开着的 SVG / MathML 元素。
type notifyForeignElement struct {
	name      string
	svg       bool // false 即 MathML
	htmlPoint bool // SVG 的 foreignObject / desc / title 与 MathML 的 annotation-xml：里面的开始标签和文字按 HTML 处理
	mathText  bool // MathML 的 mi / mo / mn / ms / mtext：里面除 mglyph / malignmark 外的开始标签和文字按 HTML 处理
	skip      bool // 浏览器不显示的外来元素：SVG 的 title / desc / style / script，MathML 的 annotation / annotation-xml
}

// notifyForeignStack 只记 SVG / MathML 元素（HTML 元素不入栈），用来判断当前标签是不是外来内容。
//
// 外来内容和 HTML 的规则不同：标签不进原始文本模式、自闭合真的闭合。tokenizer 不知道自己在 SVG 里，
// <svg><title/></svg> 会按 HTML 的 title 把后文当原始文本一直读到 </title，整段正文就没了。
// 不用 html.Parse 建树：它的开放元素栈在深层嵌套时是平方复杂度（x/net v0.33 实测 10 万层 <pre> 要 71 秒），
// 通知正文来自脚本，不能被一段畸形 HTML 卡住。这里每个元素最多入栈、出栈各一次，整体线性。
type notifyForeignStack struct {
	open   []notifyForeignElement
	counts map[string]int // 栈里各名字的个数：结束标签在栈里没有同名元素时不必扫栈，避免畸形输入退化成平方复杂度
	hidden int            // 栈里 skip 元素的个数，大于 0 时文字不输出；和 HTML 的跳过深度分开记，互相的结束标签扣不到对方
}

func (s *notifyForeignStack) active() bool {
	return len(s.open) > 0
}

// startTagIsForeign 判断这个开始标签是否按外来内容处理（对应 x/net/html 的 inForeignContent）。
func (s *notifyForeignStack) startTagIsForeign(tag atom.Atom) bool {
	if len(s.open) == 0 {
		return false
	}
	top := s.open[len(s.open)-1]
	switch {
	case top.htmlPoint:
		return false
	case top.mathText && tag != atom.Mglyph && tag != atom.Malignmark:
		return false
	}
	return true
}

// push 打开一个外来元素。svg 表示元素所在的命名空间。
func (s *notifyForeignStack) push(name []byte, tag atom.Atom, svg bool) {
	element := notifyForeignElement{name: string(name), svg: svg}
	if svg {
		element.htmlPoint = tag == atom.Foreignobject || tag == atom.Desc || tag == atom.Title
		element.skip = tag == atom.Title || tag == atom.Desc || tag == atom.Style || tag == atom.Script
	} else {
		element.mathText = tag == atom.Mi || tag == atom.Mo || tag == atom.Mn || tag == atom.Ms || tag == atom.Mtext
		// 标准里 annotation-xml 只有 encoding 是 text/html 一类时才是 HTML 集成点；它的内容反正不显示，一律按集成点处理，
		// 省得读属性，也不会因为里面的 <p> 触发跳出、把本该隐藏的内容放出来。
		element.htmlPoint = tag == atom.AnnotationXml
		element.skip = tag == atom.Annotation || tag == atom.AnnotationXml
	}
	if s.counts == nil {
		s.counts = map[string]int{}
	}
	s.counts[element.name]++
	if element.skip {
		s.hidden++
	}
	s.open = append(s.open, element)
}

// pushChild 在外来内容里打开子元素：命名空间跟随当前元素。
func (s *notifyForeignStack) pushChild(name []byte, tag atom.Atom) {
	s.push(name, tag, s.open[len(s.open)-1].svg)
}

func (s *notifyForeignStack) pop() notifyForeignElement {
	top := s.open[len(s.open)-1]
	s.open = s.open[:len(s.open)-1]
	s.counts[top.name]--
	if top.skip {
		s.hidden--
	}
	return top
}

// popTo 处理结束标签：栈里有同名元素就把它和它上面的元素一起关掉，返回是否命中。
func (s *notifyForeignStack) popTo(name []byte) bool {
	if s.counts[string(name)] == 0 {
		return false
	}
	for len(s.open) > 0 {
		if s.pop().name == string(name) {
			break
		}
	}
	return true
}

// breakout 关掉最近的集成点以上的外来元素（没有集成点就全关）。
func (s *notifyForeignStack) breakout() {
	for len(s.open) > 0 {
		top := s.open[len(s.open)-1]
		if top.htmlPoint || top.mathText {
			return
		}
		s.pop()
	}
}

// popToDepth 关掉栈里第 depth 个以上的外来元素（</template> 用）。
func (s *notifyForeignStack) popToDepth(depth int) {
	for len(s.open) > depth {
		s.pop()
	}
}

// notifyHTMLToText 把 HTML 正文转成给纯文本渠道看的文字，尽量贴近浏览器里看到的样子：
// 块级标签与 br 断行，同一行的 td / th 用「 | 」隔开，script / style 丢弃，实体反转义，
// 标签之间的源码缩进与换行按 HTML 规则折叠成一个空格（pre 里保留原样），最后合并多余空行。
func notifyHTMLToText(raw string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(raw))
	var out notifyPlainTextBuilder
	var foreign notifyForeignStack
	skipDepth := 0 // 打开着的 HTML 跳过元素
	preDepth := 0
	cellIndex := 0
	// rawSkip 是正按原始文本读的 HTML 跳过元素：tokenizer 读完它的文字后，下一个结束标签一定是它自己的，
	// 不能让同名的外来元素（如 svg 的 title）把它截走，否则 skipDepth 减不回来、后文被整段吞掉。
	var rawSkip atom.Atom
	// 每个打开着的 HTML template 开始时外来元素栈的深度：</template> 要连同模板里打开的 SVG / MathML 一起关掉。
	var templateForeignDepths []int
	hidden := func() bool { return skipDepth > 0 || foreign.hidden > 0 }

	for {
		// 外来内容里 <![CDATA[...]]> 是文字，HTML 里是注释（与 html.Parse 一致）。
		tokenizer.AllowCDATA(foreign.active())
		tokenType := tokenizer.Next()
		switch tokenType {
		case html.ErrorToken:
			// io.EOF 与解析错误都在这里收尾：tokenizer 对残缺 HTML 很宽容，走到这里已经尽力取完了文字。
			return out.finish()
		case html.TextToken:
			if hidden() {
				continue
			}
			out.writeText(string(tokenizer.Text()), preDepth > 0)
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			tag := atom.Lookup(name)
			if foreign.startTagIsForeign(tag) {
				if !notifyHTMLForeignBreakoutAtoms[tag] {
					// 外来内容里没有原始文本元素，自闭合就是空元素；也不做块级断行、表格分隔。
					tokenizer.NextIsNotRawText()
					if tokenType == html.StartTagToken {
						foreign.pushChild(name, tag)
					}
					continue
				}
				foreign.breakout()
			}
			if tag == atom.Svg || tag == atom.Math {
				if tokenType == html.StartTagToken {
					foreign.push(name, tag, tag == atom.Svg)
				}
				continue
			}
			if notifyHTMLSkippedAtoms[tag] {
				// <script/> 这类自闭合写法同样计深度：tokenizer 照样把后面到 </script> 为止的内容当原始文本，
				// 浏览器也不认 HTML 元素的自闭合，这段内容不该漏进正文。
				skipDepth++
				if tag == atom.Template {
					templateForeignDepths = append(templateForeignDepths, len(foreign.open))
				} else {
					rawSkip = tag
				}
				continue
			}
			if hidden() {
				continue
			}
			switch {
			case tag == atom.Br:
				out.lineBreak()
			case tag == atom.Td || tag == atom.Th:
				if cellIndex > 0 {
					out.separator(" | ")
				}
				cellIndex++
			case notifyHTMLBlockAtoms[tag]:
				out.blockBreak()
				if tag == atom.Tr {
					cellIndex = 0
				}
				if tag == atom.Pre && tokenType == html.StartTagToken {
					preDepth++
				}
			}
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			tag := atom.Lookup(name)
			if rawSkip != 0 && tag == rawSkip {
				rawSkip = 0
				skipDepth--
				continue
			}
			if tag == atom.P || tag == atom.Br {
				// 外来内容里的 </p> </br> 和 <p> 开始标签一样跳出到最近的集成点（HTML 标准「外来内容里的结束标签」），
				// 没闭合的 svg style、MathML annotation 不能把后文整段藏掉；随后按 HTML 结束标签照常处理。
				foreign.breakout()
			}
			if foreign.popTo(name) {
				continue
			}
			if notifyHTMLSkippedAtoms[tag] {
				// 原始文本元素都在上面按 rawSkip 收掉了，走到这里的只可能是 template，或者多余的结束标签（不理）。
				if tag == atom.Template && skipDepth > 0 {
					skipDepth--
					// 浏览器关 template 时连同模板里打开的 SVG / MathML 一起关掉；留着的话后面的 script 会被当成外来元素，内容漏出来。
					if n := len(templateForeignDepths); n > 0 {
						foreign.popToDepth(templateForeignDepths[n-1])
						templateForeignDepths = templateForeignDepths[:n-1]
					}
				}
				continue
			}
			if hidden() {
				continue
			}
			if notifyHTMLBlockAtoms[tag] {
				out.blockBreak()
				if tag == atom.Tr {
					cellIndex = 0
				}
				if tag == atom.Pre && preDepth > 0 {
					preDepth--
				}
			}
		}
	}
}

// notifyPlainTextBuilder 负责折叠空白：行首不写空格、连续空白只留一个，断行前去掉行尾空格。
type notifyPlainTextBuilder struct {
	buf []byte
}

func (b *notifyPlainTextBuilder) atLineStart() bool {
	return len(b.buf) == 0 || b.buf[len(b.buf)-1] == '\n'
}

func (b *notifyPlainTextBuilder) trimTrailingSpaces() {
	for len(b.buf) > 0 && (b.buf[len(b.buf)-1] == ' ' || b.buf[len(b.buf)-1] == '\t') {
		b.buf = b.buf[:len(b.buf)-1]
	}
}

// lineBreak 是 br：每个都换一行，连续两个 br 就是一个空行。
func (b *notifyPlainTextBuilder) lineBreak() {
	b.trimTrailingSpaces()
	b.buf = append(b.buf, '\n')
}

// blockBreak 是块级标签的边界：已经在行首就不再换行，否则表格每一行、每个 div 之间都会多出空行。
func (b *notifyPlainTextBuilder) blockBreak() {
	b.trimTrailingSpaces()
	if !b.atLineStart() {
		b.buf = append(b.buf, '\n')
	}
}

func (b *notifyPlainTextBuilder) separator(sep string) {
	b.trimTrailingSpaces()
	if !b.atLineStart() {
		b.buf = append(b.buf, sep...)
	}
}

func (b *notifyPlainTextBuilder) writeText(text string, preformatted bool) {
	if preformatted {
		b.buf = append(b.buf, strings.ReplaceAll(text, "\r\n", "\n")...)
		return
	}
	lastSpace := b.atLineStart() || b.buf[len(b.buf)-1] == ' '
	for _, r := range text {
		switch r {
		case ' ', '\t', '\n', '\r', '\f':
			if !lastSpace {
				b.buf = append(b.buf, ' ')
				lastSpace = true
			}
		case '\u00a0':
			// &nbsp; 在浏览器里不参与折叠，这里同样保留成一个普通空格。
			b.buf = append(b.buf, ' ')
			lastSpace = false
		default:
			b.buf = append(b.buf, string(r)...)
			lastSpace = false
		}
	}
}

// finish 去掉每行行尾空白，连续空行最多保留一个，首尾空行去掉。
func (b *notifyPlainTextBuilder) finish() string {
	lines := strings.Split(string(b.buf), "\n")
	result := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			if len(result) > 0 && !blank {
				result = append(result, "")
			}
			blank = true
			continue
		}
		result = append(result, line)
		blank = false
	}
	return strings.TrimRight(strings.Join(result, "\n"), "\n")
}
