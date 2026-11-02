package service

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"regexp/syntax"
	"strings"

	"daidai-panel/model"
)

// 订阅的白名单 / 黑名单 / 依赖规则三个字段共用的片段拆分、分类与匹配（#129）。
//
// 口径（向青龙 `ql repo` 的 `grep -E` 靠拢，同时保住呆呆的存量配置）：
//   - 多个片段用 `,` 或 `|` 分隔，只在「顶层」拆：有配对的圆括号 / 花括号里、方括号表达式里、
//     被 `\` 转义的分隔符都不拆，所以 `(jd|jx)_`、`a{1,3}`、`[,|]` 各是一整段；
//   - 片段里出现 ^ $ ( ) [ ] { } ? \ 任一字符，或含 `.*` / `.+`，按正则（Go RE2）匹配仓库相对路径：
//     正斜杠、不锚定的 MatchString，与青龙 `egrep` 同口径（`^jd[^_]` 只命中仓库根下 jdCookie.js 这类文件）；
//   - 其余片段保持「子串包含」，行为与改造前逐字节一致：c++、a.b、jd_*.js、.github 这类普通写法都不受影响；
//   - `* / ** / *.* / .* / all / 全部` 这些通配写法、依赖规则的「文字备注」判定，照旧先于分类执行。
//
// 「指定子目录」不是正则字段，仍按精确路径处理，不走这里（见 splitSubscriptionSparseTargets）。
//
// 普通片段的行为由 subscription_pattern_pin_test.go 钉着（golden 来自改造前的实现）。

// subscriptionRegexTriggerChars：片段里出现其中任一字符就按正则解析；`.*` / `.+` 两个组合另算。
// 单独的 `+` `.` `*` 不算，否则 c++、a.b、jd_*.js 这类存量写法会凭空变成正则。
const subscriptionRegexTriggerChars = `^$()[]{}?\`

// subscriptionRegexTriggerHint 是给用户看的可读版本。
const subscriptionRegexTriggerHint = "^ $ ( ) [ ] { } ? \\ 或 .* .+"

func isSubscriptionRegexFragment(fragment string) bool {
	return strings.ContainsAny(fragment, subscriptionRegexTriggerChars) ||
		strings.Contains(fragment, ".*") ||
		strings.Contains(fragment, ".+")
}

type subscriptionPatternField int

const (
	subscriptionPatternWhitelist subscriptionPatternField = iota
	subscriptionPatternBlacklist
	subscriptionPatternDepend
)

func (f subscriptionPatternField) label() string {
	switch f {
	case subscriptionPatternWhitelist:
		return "白名单"
	case subscriptionPatternBlacklist:
		return "黑名单"
	default:
		return "依赖规则"
	}
}

// 保存订阅时要校验正则的三个字段（请求体里的 JSON 键名）。
const (
	SubscriptionFilterFieldWhitelist = "whitelist"
	SubscriptionFilterFieldBlacklist = "blacklist"
	SubscriptionFilterFieldDependOn  = "depend_on"
)

func subscriptionPatternFieldByKey(key string) (subscriptionPatternField, bool) {
	switch key {
	case SubscriptionFilterFieldWhitelist:
		return subscriptionPatternWhitelist, true
	case SubscriptionFilterFieldBlacklist:
		return subscriptionPatternBlacklist, true
	case SubscriptionFilterFieldDependOn:
		return subscriptionPatternDepend, true
	}
	return 0, false
}

// ValidateSubscriptionFilterField 在保存时校验一个过滤字段（key 取 SubscriptionFilterField* 之一），
// 只对识别成正则的片段编译；通配写法、依赖规则的文字备注、普通片段一律放行。
// 非法时返回的文案点名字段、第几段、RE2 的报错，并提示怎么写字面量。
//
// 拉取时对存量数据（升级前保存的、青龙备份导入的）另有兜底：编译失败的片段逐条跳过并打 [警告]，
// 见 subscriptionPatternSet.invalidWarning。
func ValidateSubscriptionFilterField(key, raw string) error {
	field, ok := subscriptionPatternFieldByKey(key)
	if !ok {
		return nil
	}
	set := compileSubscriptionPatternSet(raw, field)
	invalid := set.invalidFragments()
	if len(invalid) == 0 {
		return nil
	}
	first := invalid[0]
	return fmt.Errorf("%s第 %d 段 `%s` 不是合法的正则表达式：%s（含 %s 的片段按正则解析，要写字面量请用 \\ 转义）",
		field.label(), first.ordinal, first.raw, first.error, subscriptionRegexTriggerHint)
}

// ValidateSubscriptionFilterFields 依次校验白名单、黑名单、依赖规则，返回第一处错误。
func ValidateSubscriptionFilterFields(whitelist, blacklist, dependOn string) error {
	for _, item := range []struct{ key, value string }{
		{SubscriptionFilterFieldWhitelist, whitelist},
		{SubscriptionFilterFieldBlacklist, blacklist},
		{SubscriptionFilterFieldDependOn, dependOn},
	} {
		if err := ValidateSubscriptionFilterField(item.key, item.value); err != nil {
			return err
		}
	}
	return nil
}

func describeSubscriptionRegexError(err error) string {
	var syntaxErr *syntax.Error
	if errors.As(err, &syntaxErr) {
		return fmt.Sprintf("%s: `%s`", syntaxErr.Code, syntaxErr.Expr)
	}
	return err.Error()
}

// splitSubscriptionPatternSegments 把字段拆成非空片段（已 TrimSpace），保持书写顺序、不去重
// （保存时报「第几段」要按用户写的顺序数，去重交给调用方）。
//
// 只在顶层的 `,` `|` 处拆：
//   - `\` 转义下一个字符：`a\|b` 是一段；
//   - 方括号表达式整体算一段：`[,|]x`；紧跟 `[` 或 `[^` 的 `]` 是字面量，`[:alpha:]` 这类具名类里的 `]`
//     不结束外层，与 RE2 / POSIX 的解析一致；
//   - 有配对的 `( )`、`{ }` 里面不拆：`(jd|jx)_`、`a{1,3}`。
//
// 没有配对的括号（`(jd|USER`、`^jd[|USER`）当普通字符、照常拆开：这类片段本来就编译不过，
// 拆开之后只坏它自己那一段，同一字段里其余合法片段照常生效（拉取时逐条跳过，保存时点名那一段）。
//
// 不含 `( [ { \` 的输入，结果与改造前 strings.FieldsFunc 按 `,` `|` 拆逐字节一致。
func splitSubscriptionPatternSegments(raw string) []string {
	runes := []rune(raw)
	// role[i] 非 0 表示 runes[i] 是结构字符：分隔符或括号。被转义的字符、方括号表达式里的字符一律当普通字符。
	role := make([]rune, len(runes))
	for i := 0; i < len(runes); i++ {
		switch r := runes[i]; r {
		case '\\':
			i++
		case '[':
			if end := subscriptionBracketExprEnd(runes, i); end >= 0 {
				i = end
			}
		case '(', ')', '{', '}', ',', '|':
			role[i] = r
		}
	}

	// 只有配对的括号才参与「里面不拆」；落单的开 / 闭括号退回普通字符。
	var openParens, openBraces []int
	for i, r := range role {
		switch r {
		case '(':
			openParens = append(openParens, i)
		case '{':
			openBraces = append(openBraces, i)
		case ')':
			if n := len(openParens); n > 0 {
				openParens = openParens[:n-1]
			} else {
				role[i] = 0
			}
		case '}':
			if n := len(openBraces); n > 0 {
				openBraces = openBraces[:n-1]
			} else {
				role[i] = 0
			}
		}
	}
	for _, i := range openParens {
		role[i] = 0
	}
	for _, i := range openBraces {
		role[i] = 0
	}

	var segments []string
	start, depth := 0, 0
	flush := func(end int) {
		if segment := strings.TrimSpace(string(runes[start:end])); segment != "" {
			segments = append(segments, segment)
		}
	}
	for i, r := range role {
		switch r {
		case '(', '{':
			depth++
		case ')', '}':
			depth--
		case ',', '|':
			if depth == 0 {
				flush(i)
				start = i + 1
			}
		}
	}
	flush(len(runes))
	return segments
}

// subscriptionBracketExprEnd 返回从 runes[start]（必须是 `[`）开始的方括号表达式的闭合 `]` 下标，没闭合返回 -1。
func subscriptionBracketExprEnd(runes []rune, start int) int {
	i := start + 1
	if i < len(runes) && runes[i] == '^' {
		i++
	}
	if i < len(runes) && runes[i] == ']' {
		i++
	}
	for ; i < len(runes); i++ {
		switch runes[i] {
		case '\\':
			i++
		case '[':
			if i+1 < len(runes) && runes[i+1] == ':' {
				for j := i + 2; j+1 < len(runes); j++ {
					if runes[j] == ':' && runes[j+1] == ']' {
						i = j + 1
						break
					}
				}
			}
		case ']':
			return i
		}
	}
	return -1
}

type subscriptionFragmentKind int

const (
	// subscriptionFragmentWildcard：通配写法（依赖规则里还包括规整后为空的片段）。
	// 白名单里等于「全部命中」，黑名单、依赖规则里跳过。
	subscriptionFragmentWildcard subscriptionFragmentKind = iota
	// subscriptionFragmentNote：依赖规则里的文字备注，跳过并在日志里点名。
	subscriptionFragmentNote
	// subscriptionFragmentSubstring：普通片段，子串包含。
	subscriptionFragmentSubstring
	// subscriptionFragmentRegex：正则片段。
	subscriptionFragmentRegex
	// subscriptionFragmentInvalid：按正则解析但编译失败，逐条跳过并告警。
	subscriptionFragmentInvalid
)

type subscriptionFragment struct {
	kind    subscriptionFragmentKind
	ordinal int    // 第几段：从 1 数，按用户写的非空片段计数（不去重）
	raw     string // 拆分后的原文
	// text：子串片段拿去做 subscriptionFilterContains 的文本——白 / 黑名单是原文、依赖规则是规整后的文本，
	// 与改造前两边各自的调用口径逐字一致；正则片段是原文；备注是日志里点名的文本。
	text string
	// sparse：子串片段下发给 sparse-checkout 的片段（已规整；空串表示不下发）。
	sparse string
	re     *regexp.Regexp
	error  string
}

func (f *subscriptionFragment) matches(filePath string) bool {
	switch f.kind {
	case subscriptionFragmentSubstring:
		return subscriptionFilterContains(filePath, f.text)
	case subscriptionFragmentRegex:
		target := normalizeSubscriptionFilterTarget(filePath)
		return target != "" && f.re.MatchString(target)
	}
	return false
}

// subscriptionPatternSet 是一个字段编译好的全部片段。一次拉取 / 同步只编译一次：
// 以前白名单对每个文件都要重新拆一遍，加了正则编译以后必须缓存。
type subscriptionPatternSet struct {
	field     subscriptionPatternField
	fragments []subscriptionFragment
}

func compileSubscriptionPatternSet(raw string, field subscriptionPatternField) subscriptionPatternSet {
	set := subscriptionPatternSet{field: field}
	seen := make(map[string]bool)
	for i, segment := range splitSubscriptionPatternSegments(raw) {
		if seen[segment] {
			continue
		}
		seen[segment] = true
		set.fragments = append(set.fragments, classifySubscriptionFragment(segment, i+1, field))
	}
	return set
}

func classifySubscriptionFragment(segment string, ordinal int, field subscriptionPatternField) subscriptionFragment {
	frag := subscriptionFragment{ordinal: ordinal, raw: segment, text: segment}
	if isWildcardFilterPattern(segment) {
		frag.kind = subscriptionFragmentWildcard
		return frag
	}

	if isSubscriptionRegexFragment(segment) {
		// 正则片段只 TrimSpace（拆分时已做），不走 normalizeSubscriptionFilterTarget：
		// Windows 上 ToSlash 会把 `\.` 改写成 `/.`，去掉 `./` 前缀也会改写 `./x` 的含义。
		if field == subscriptionPatternDepend && looksLikeSubscriptionDependencyNote(segment) {
			frag.kind = subscriptionFragmentNote
			return frag
		}
		re, err := regexp.Compile(segment)
		if err != nil {
			frag.kind = subscriptionFragmentInvalid
			frag.error = describeSubscriptionRegexError(err)
			return frag
		}
		frag.kind = subscriptionFragmentRegex
		frag.re = re
		return frag
	}

	if field == subscriptionPatternDepend {
		// 依赖规则的普通片段沿用改造前 splitSubscriptionDependencyPatterns 的顺序：
		// 先规整，再判空 / 通配，再判备注；参与匹配与下发的都是规整后的文本。
		normalized := normalizeSubscriptionFilterTarget(segment)
		frag.text = normalized
		switch {
		case normalized == "" || isWildcardFilterPattern(normalized):
			frag.kind = subscriptionFragmentWildcard
		case looksLikeSubscriptionDependencyNote(normalized):
			frag.kind = subscriptionFragmentNote
		default:
			frag.kind = subscriptionFragmentSubstring
			frag.sparse = normalized
		}
		return frag
	}

	// 白 / 黑名单的普通片段：匹配用原文（subscriptionFilterContains 内部再规整），
	// 下发 sparse 用规整后的文本，规整后为空或成了通配写法就不下发——两处都与改造前一致。
	frag.kind = subscriptionFragmentSubstring
	if normalized := normalizeSubscriptionFilterTarget(segment); normalized != "" && !isWildcardFilterPattern(normalized) {
		frag.sparse = normalized
	}
	return frag
}

func (s *subscriptionPatternSet) hasRegex() bool {
	for i := range s.fragments {
		if s.fragments[i].kind == subscriptionFragmentRegex {
			return true
		}
	}
	return false
}

// hasNonWildcard：填了通配写法以外的东西（含编译失败的正则片段）。
func (s *subscriptionPatternSet) hasNonWildcard() bool {
	for i := range s.fragments {
		if s.fragments[i].kind != subscriptionFragmentWildcard {
			return true
		}
	}
	return false
}

func (s *subscriptionPatternSet) regexFragments() []string {
	var out []string
	for i := range s.fragments {
		if s.fragments[i].kind == subscriptionFragmentRegex {
			out = append(out, s.fragments[i].raw)
		}
	}
	return out
}

func (s *subscriptionPatternSet) invalidFragments() []subscriptionFragment {
	var out []subscriptionFragment
	for i := range s.fragments {
		if s.fragments[i].kind == subscriptionFragmentInvalid {
			out = append(out, s.fragments[i])
		}
	}
	return out
}

func (s *subscriptionPatternSet) notes() []string {
	var out []string
	for i := range s.fragments {
		if s.fragments[i].kind == subscriptionFragmentNote {
			out = append(out, s.fragments[i].text)
		}
	}
	return out
}

// sparseTargets：能下发给 sparse-checkout 的普通片段（规整后）。正则片段表达不成 gitignore 规则，不在此列。
func (s *subscriptionPatternSet) sparseTargets() []string {
	var out []string
	for i := range s.fragments {
		if f := &s.fragments[i]; f.kind == subscriptionFragmentSubstring && f.sparse != "" {
			out = append(out, f.sparse)
		}
	}
	return out
}

// whitelistMatches：有通配片段就全部命中；留空（没有任何非通配片段）也全部命中；否则任一片段命中即可。
// 编译失败的正则片段算「填了、但一个文件都匹配不到」——与改造前把它当字面量子串匹配的结果一致，
// 所以白名单只剩这种片段时照旧走兜底 #2。
func (s *subscriptionPatternSet) whitelistMatches(filePath string) bool {
	hasNonWildcard := false
	for i := range s.fragments {
		f := &s.fragments[i]
		if f.kind == subscriptionFragmentWildcard {
			return true
		}
		hasNonWildcard = true
		if f.matches(filePath) {
			return true
		}
	}
	return !hasNonWildcard
}

// anyMatches：任一片段命中（通配、备注、编译失败的片段都不算）。黑名单与依赖规则用它。
func (s *subscriptionPatternSet) anyMatches(filePath string) bool {
	for i := range s.fragments {
		if s.fragments[i].matches(filePath) {
			return true
		}
	}
	return false
}

// invalidWarning 是拉取时对非法正则片段的兜底告警（存量数据、青龙备份导入绕过了保存时的校验）。
func (s *subscriptionPatternSet) invalidWarning() string {
	invalid := s.invalidFragments()
	if len(invalid) == 0 {
		return ""
	}
	parts := make([]string, 0, len(invalid))
	for _, f := range invalid {
		parts = append(parts, fmt.Sprintf("第 %d 段 `%s`（%s）", f.ordinal, f.raw, f.error))
	}
	consequence := ""
	switch s.field {
	case subscriptionPatternWhitelist:
		consequence = "，这些片段匹配不到任何文件"
		usable := false
		for i := range s.fragments {
			if s.fragments[i].kind != subscriptionFragmentInvalid {
				usable = true
				break
			}
		}
		if !usable {
			consequence += "；白名单已经没有可用的片段，本次按「白名单一个文件都没命中」处理"
		}
	case subscriptionPatternBlacklist:
		consequence = "，对应的文件不会被排除"
	default:
		consequence = "，对应的依赖文件不会被识别"
	}
	return fmt.Sprintf("[警告] %s%s 不是合法的正则表达式，已逐条跳过%s。含 %s 的片段按正则解析，要写字面量请用 \\ 转义",
		s.field.label(), strings.Join(parts, "、"), consequence, subscriptionRegexTriggerHint)
}

// subscriptionInvalidPatternWarnings 收集几个字段的非法正则告警（没有就返回 nil）。
func subscriptionInvalidPatternWarnings(sets ...*subscriptionPatternSet) []string {
	var warnings []string
	for _, set := range sets {
		if warning := set.invalidWarning(); warning != "" {
			warnings = append(warnings, warning)
		}
	}
	return warnings
}

// subscriptionIncludePlan 是包含侧（指定子目录 / 白名单）在 sparse-checkout 里的处理方式。
// buildSubscriptionSparseCheckoutPatterns（决定落盘）和 newSubscriptionFilterMatcher（决定建任务）
// 都从 resolveSubscriptionIncludePlan 取，不许各算各的：两边一旦不一致，就会出现
// 「整仓落了盘、建任务的范围却没有收回来」这种不对称。
type subscriptionIncludePlan int

const (
	subscriptionIncludeNone           subscriptionIncludePlan = iota // 子目录、白名单都没有可下发的规则：整仓
	subscriptionIncludeUnsafeSubPath                                 // 子目录含 git 通配特殊字符：整仓
	subscriptionIncludeSubPath                                       // 按指定子目录（精确路径）检出
	subscriptionIncludeWhitelistRegex                                // 白名单含正则片段：整仓，由 Go 侧按白名单筛任务
	subscriptionIncludeWhitelist                                     // 按白名单普通片段检出
)

func resolveSubscriptionIncludePlan(subPaths, unsafeSubPaths []string, whitelist *subscriptionPatternSet) subscriptionIncludePlan {
	switch {
	case len(unsafeSubPaths) > 0:
		return subscriptionIncludeUnsafeSubPath
	case len(subPaths) > 0:
		return subscriptionIncludeSubPath
	case whitelist.hasRegex():
		return subscriptionIncludeWhitelistRegex
	case len(whitelist.sparseTargets()) > 0:
		return subscriptionIncludeWhitelist
	default:
		return subscriptionIncludeNone
	}
}

// restrictsCheckout：包含侧真的给 sparse-checkout 下发了规则（只落一部分文件）。
// 等价于改造前 buildSubscriptionSparseCheckoutPatterns 里「依赖规则守卫」的 len(patterns) > 0。
func (p subscriptionIncludePlan) restrictsCheckout() bool {
	return p == subscriptionIncludeSubPath || p == subscriptionIncludeWhitelist
}

// subscriptionFilterMatcher 是一次同步里判断「建不建任务」要用到的全部规则：
// 构造时把三个字段各编译一次，之后对每个文件只做匹配。
type subscriptionFilterMatcher struct {
	whitelist subscriptionPatternSet
	blacklist subscriptionPatternSet
	depend    subscriptionPatternSet
	// subPathScope 非空时，只有落在这些子目录里的文件才建任务，见 newSubscriptionFilterMatcher。
	subPathScope []string
	// widenedByDependRegex：包含侧本来有 sparse 限制，只因为依赖规则里有正则片段才检出了整个仓库。
	widenedByDependRegex bool
	// dependBeyondSubPath：按指定子目录检出，依赖规则的普通片段并进了检出范围，子目录外命中依赖规则的文件也会落盘。
	dependBeyondSubPath bool
	// widenedByWhitelistRegex：没有指定子目录、白名单含正则片段，本次检出了整个仓库（完整检出开关没开）。
	widenedByWhitelistRegex bool
}

// newSubscriptionFilterMatcher 编译订阅的过滤规则。
//
// 子目录护栏（subPathScope）：`sub_path` 对「哪些文件建任务」的约束，一直是靠
// 「不在子目录里的文件根本不落盘」间接实现的。有三种情况子目录外的文件也落了盘，这条间接约束随之失效，要在这里补回来：
//   - 打开了「完整检出」：一个填了 `sub_path=qinglong/DefaultTasks`、白名单留空、开了自动建任务的订阅，
//     会把仓库里每个 .sh/.js/.py（含 tools/、examples/、ci/ 下的）都建成定时任务并真的按 cron 跑起来，
//     而「自动删除失效任务」不会帮用户收回去——那一项只删「订阅源里已经消失的脚本」对应的任务；
//   - 依赖规则含正则片段、把检出放宽成了整仓（#129 档 1）：用户只是想多拉一个 jdCookie.js，不是想多建一堆任务；
//   - 依赖规则只有普通片段、并进了子目录的 sparse 规则：子目录外命中依赖规则的文件会落盘。白名单留空时每个文件都算
//     命中白名单，isDependencyOnly 一个都摘不掉，没有护栏的话这些依赖文件会被建成定时任务
//     （真机 git 用例见 subscription_subpath_depend_scope_test.go）。
//
// 子目录含 git 元字符（risky）时 sparse 那边本来就退回整仓、不做限制，这里也不加约束，
// 否则会出现「文件全落盘、任务却一个都不建」这种更难查的不对称。
func newSubscriptionFilterMatcher(sub *model.Subscription) *subscriptionFilterMatcher {
	m := &subscriptionFilterMatcher{
		whitelist: subscriptionPatternSet{field: subscriptionPatternWhitelist},
		blacklist: subscriptionPatternSet{field: subscriptionPatternBlacklist},
		depend:    subscriptionPatternSet{field: subscriptionPatternDepend},
	}
	if sub == nil {
		return m
	}
	m.whitelist = compileSubscriptionPatternSet(sub.Whitelist, subscriptionPatternWhitelist)
	m.blacklist = compileSubscriptionPatternSet(sub.Blacklist, subscriptionPatternBlacklist)
	m.depend = compileSubscriptionPatternSet(sub.DependOn, subscriptionPatternDepend)

	subPaths, unsafeSubPaths := splitSubscriptionSparseTargets(sub.SubPath)
	plan := resolveSubscriptionIncludePlan(subPaths, unsafeSubPaths, &m.whitelist)
	m.widenedByDependRegex = !sub.FullCheckout && plan.restrictsCheckout() && m.depend.hasRegex()
	// 与 buildSubscriptionSparseCheckoutPatterns 依赖分支的条件一致：只有依赖片段真的并进了 sparse 规则，子目录外才会多落文件。
	m.dependBeyondSubPath = !sub.FullCheckout && plan == subscriptionIncludeSubPath && !m.depend.hasRegex() && len(m.depend.sparseTargets()) > 0
	m.widenedByWhitelistRegex = !sub.FullCheckout && plan == subscriptionIncludeWhitelistRegex
	if (sub.FullCheckout || m.widenedByDependRegex || m.dependBeyondSubPath) && len(unsafeSubPaths) == 0 && len(subPaths) > 0 {
		m.subPathScope = subPaths
	}
	return m
}

func (m *subscriptionFilterMatcher) matchesWhitelist(filePath string) bool {
	return m.whitelist.whitelistMatches(filePath)
}

// passesBlacklist：黑名单一条都没命中。
func (m *subscriptionFilterMatcher) passesBlacklist(filePath string) bool {
	return !m.blacklist.anyMatches(filePath)
}

func (m *subscriptionFilterMatcher) matchesFilters(filePath string) bool {
	return m.matchesWhitelist(filePath) && m.passesBlacklist(filePath)
}

func (m *subscriptionFilterMatcher) matchesDependency(filePath string) bool {
	return m.depend.anyMatches(filePath)
}

// isDependencyOnly：只因为依赖规则才落盘的辅助库文件（白名单优先，见 isSubscriptionDependencyOnlyFile）。
func (m *subscriptionFilterMatcher) isDependencyOnly(filePath string) bool {
	return !m.matchesWhitelist(filePath) && m.matchesDependency(filePath)
}

func (m *subscriptionFilterMatcher) inSubPathScope(filePath string) bool {
	if len(m.subPathScope) == 0 {
		return true
	}
	rel := strings.TrimPrefix(filepath.ToSlash(filePath), "./")
	for _, target := range m.subPathScope {
		if subscriptionSubPathCovers(target, rel) {
			return true
		}
	}
	return false
}

// subscriptionSubPathCovers 判断「指定子目录」里的一条路径（已规整、不含 ? [ ] \）会不会让 rel
// （仓库相对路径，正斜杠）落盘，口径与 git sparse-checkout --no-cone 解释同一条规则时一致：
//   - 末尾的 `/` 只表示「这是目录」：`scripts/daily/` 与 `scripts/daily` 覆盖同一批文件；
//   - 去掉末尾 `/` 后仍含 `/` 的锚定在仓库根，否则在任意层级匹配（`daily` 也命中 scripts/daily/）；
//   - `*` 按 glob 匹配一段、不跨 `/`，整段的 `**` 匹配任意层；
//   - 目录被命中，目录里的全部内容都算命中。
//
// 子目录护栏只在子目录外的文件也落了盘时生效（完整检出、依赖规则的正则片段放宽检出、依赖规则的普通片段并进检出范围），
// 它的用途是把建任务的范围限回「sparse 本来会检出的那些文件」。所以它必须与 git 同口径：比 sparse 严一点，就会出现
// 「改造前能建的任务，放宽检出之后一个都不建」——`scripts/daily/`、`scripts/*`、`daily` 都踩过。
func subscriptionSubPathCovers(target, rel string) bool {
	dirOnly := strings.HasSuffix(target, "/")
	target = strings.TrimRight(target, "/")
	if target == "" {
		return true
	}
	relParts := strings.Split(rel, "/")
	// rel 自己是文件：规则带末尾 `/` 时只能靠它的某一级父目录命中。
	limit := len(relParts)
	if dirOnly {
		limit--
	}
	if !strings.Contains(target, "/") {
		for _, part := range relParts[:limit] {
			if ok, _ := path.Match(target, part); ok {
				return true
			}
		}
		return false
	}
	targetParts := strings.Split(target, "/")
	for end := 1; end <= limit; end++ {
		if matchSubscriptionSubPathParts(targetParts, relParts[:end]) {
			return true
		}
	}
	return false
}

// matchSubscriptionSubPathParts 逐段匹配：`**` 整段匹配零到多段，其余段按 path.Match（`*` 不跨 `/`）。
func matchSubscriptionSubPathParts(pattern, parts []string) bool {
	if len(pattern) == 0 {
		return len(parts) == 0
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(parts); i++ {
			if matchSubscriptionSubPathParts(pattern[1:], parts[i:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	if ok, _ := path.Match(pattern[0], parts[0]); !ok {
		return false
	}
	return matchSubscriptionSubPathParts(pattern[1:], parts[1:])
}

func (m *subscriptionFilterMatcher) shouldManage(filePath string, allowedExts map[string]bool) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	if !allowedExts[ext] {
		return false
	}
	if !m.inSubPathScope(filePath) {
		return false
	}
	return m.matchesFilters(filePath)
}

// countsTowardWhitelistFallback / allowsWhitelistFallback：兜底 #2（白名单一个都没命中 → 忽略白 / 黑名单）
// 在依赖规则的正则片段把检出放宽成整仓时，只按「放宽前本来就会落盘的范围」判断——#129 的放宽只为了
// 多落几个依赖文件，不能顺带改变建哪些任务：
//   - 有指定子目录：放宽前落盘的是子目录（加依赖文件），所以只数子目录里的文件；兜底之后建任务也仍限在子目录里；
//   - 没有子目录（放宽的是白名单普通片段）：放宽前白名单没命中的文件压根不在盘上，兜底 #2 在那种检出下
//     几乎不可能触发；放宽之后整仓都在盘上，白名单一旦写错（或仓库改了文件名），兜底会把整个仓库的脚本
//     都建成任务并真的跑起来。所以这种情况不兜底，交给「没有扫描到任何候选脚本」那几条提示。
//
// 白名单自己含正则片段、因此检出了整个仓库（没有子目录、完整检出没开）时同样不兜底：改造前 `^scripts/jd_`、
// `(jd|jx)_` 这类片段被拆成 gitignore 规则下发，一个文件都检不出来，任务也就一个不建；改造后整仓都在盘上，
// 正则一个没命中（写错、仓库改了文件名）就兜底的话，会把整个仓库的脚本都建成任务并按默认 cron 跑起来。
// 白名单只剩编译失败的片段时不算这一类（没有可用的正则，按设计维持整仓 + 兜底 #2）。
//
// 依赖规则的普通片段并进子目录的检出范围时（dependBeyondSubPath）计数口径同上：子目录外落盘的只有依赖文件，
// 只数子目录里的文件。否则白名单只命中了子目录外的依赖文件时，兜底不触发、那个文件又被子目录护栏挡掉，
// 结果一个任务都不建；按子目录计数则与依赖规则还是纯备注时（那些文件根本不落盘）结果一致。
func (m *subscriptionFilterMatcher) countsTowardWhitelistFallback(filePath string) bool {
	return !(m.widenedByDependRegex || m.dependBeyondSubPath) || m.inSubPathScope(filePath)
}

func (m *subscriptionFilterMatcher) allowsWhitelistFallback() bool {
	if m.widenedByWhitelistRegex {
		return false
	}
	return !m.widenedByDependRegex || len(m.subPathScope) > 0
}

// withoutWhiteBlacklist 对应兜底 #2：白 / 黑名单清空，子目录护栏与依赖规则原样保留。
func (m *subscriptionFilterMatcher) withoutWhiteBlacklist() *subscriptionFilterMatcher {
	fallback := *m
	fallback.whitelist = subscriptionPatternSet{field: subscriptionPatternWhitelist}
	fallback.blacklist = subscriptionPatternSet{field: subscriptionPatternBlacklist}
	return &fallback
}
