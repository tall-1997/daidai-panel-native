package service

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
)

// pep503SeparatorPattern 把连续的 - _ . 折叠成单个 -（PEP 503 规范化）。
var pep503SeparatorPattern = regexp.MustCompile(`[-_.]+`)

// CanonicalizePythonPackageName 把用户输入的 Python 依赖 spec 归一化成 PEP 503 去重键。
//
// 步骤：
//  1. 剥离 PEP 508 环境标记（`;` 之后）、extras（`[...]`）、版本算符及其后的版本号。
//  2. 对剩下的包名部分做 PEP 503 规范化：转小写 + 把连续的 -_. 折叠成单个 -。
//
// 关键：必须先拆出包名部分再折叠，绝不能对整串套折叠正则，
// 否则 `requests==2.31.0` 里版本号的 `.` 会被折成 `-`。
//
// 例：Requests->requests，Flask_SQLAlchemy->flask-sqlalchemy，
// requests==2.31.0->requests，zope.interface[test]->zope-interface。
//
// 仅供 Python/pip 依赖去重使用，不要用于 npm / Linux。
func CanonicalizePythonPackageName(spec string) string {
	name := strings.TrimSpace(spec)
	if name == "" {
		return ""
	}

	// 1. 去掉 PEP 508 环境标记（`requests; python_version < "3"`）。
	if idx := strings.IndexByte(name, ';'); idx >= 0 {
		name = name[:idx]
	}
	// 2. 去掉 extras（`requests[security]`）。
	if idx := strings.IndexByte(name, '['); idx >= 0 {
		name = name[:idx]
	}
	// 3. 去掉版本算符及其后的版本号（== >= <= ~= != === < > 以及空白/括号/逗号）。
	name = stripPythonVersionSpecifier(name)
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	// 4. PEP 503 规范化：先取到纯包名后再 lower + 折叠分隔符。
	name = strings.ToLower(name)
	name = pep503SeparatorPattern.ReplaceAllString(name, "-")
	return name
}

// PythonExtrasCovered 报告同一个基础包下，已登记的写法 existingSpec 的 extras 是否已经覆盖了
// 本次请求的写法 requestedSpec 的 extras（即旧 extras 集合 ⊇ 新 extras 集合）。
//
// 为什么需要它（issue #166）：CanonicalizePythonPackageName 会剥掉 extras，查重时 httpx[http2] 与已装的
// httpx 是同一条记录。这本身是对的（一包一条），但如果就此「已存在跳过」，h2 这个可选依赖就永远装不上；
// 自动安装登记时不改名，重建容器后按旧名字重装也会把 h2 丢掉。调用方据此决定要不要原地改名并重装。
//
// extras 名按小写 + PEP 503 分隔符折叠后比较（HTTP_2 与 http-2 视为同一个）。
// 只比 extras，不比基础名和版本号 —— 调用方已经用归一化键确认过是同一个包。
func PythonExtrasCovered(existingSpec, requestedSpec string) bool {
	// 取出一个写法里的 extras 集合：只看环境标记 `;` 之前第一对方括号里的内容。
	extrasOf := func(spec string) map[string]bool {
		extras := map[string]bool{}
		if idx := strings.IndexByte(spec, ';'); idx >= 0 {
			spec = spec[:idx]
		}
		start := strings.IndexByte(spec, '[')
		if start < 0 {
			return extras
		}
		end := strings.IndexByte(spec[start:], ']')
		if end < 0 {
			// 方括号没闭合属于写错了，按「没有 extras」处理，交给 pip 去报错。
			return extras
		}
		for _, extra := range strings.Split(spec[start+1:start+end], ",") {
			extra = strings.ToLower(strings.TrimSpace(extra))
			extra = pep503SeparatorPattern.ReplaceAllString(extra, "-")
			if extra != "" {
				extras[extra] = true
			}
		}
		return extras
	}

	existing := extrasOf(existingSpec)
	for extra := range extrasOf(requestedSpec) {
		if !existing[extra] {
			return false
		}
	}
	return true
}

// MergePythonSpecExtras 把本次写法的 extras 合并进已登记的写法，得到原地升级后的新记录名（issue #166）。
// 只在 PythonExtrasCovered 返回 false 时用。
//
// 为什么要合并而不是整串换成本次写法：记录名就是重建容器后自动重装用的写法。
// 已装 httpx==0.27.0 再补一个 httpx[http2]，若记录名直接变成 httpx[http2]，下次重装版本锁定就没了；
// 已装 httpx[socks] 再补 httpx[http2]，socks 又会丢 —— 修好了 h2，却把用户原有的东西弄丢。
//
// 规则：
//   - 基础名用本次写法的（大小写按用户新输入的来）；
//   - extras = 旧写法的 extras（保持原顺序、原拼写）+ 本次写法里旧写法没有的（比较口径同 PythonExtrasCovered）；
//   - extras 后面那一段（版本约束、`@ url`、`; marker`，原样保留前导空白）：本次写法有就用本次的，没有就沿用旧的。
//
// 例：httpx==0.27.0 + httpx[http2] -> httpx[http2]==0.27.0；httpx[socks] + httpx[http2] -> httpx[socks,http2]。
func MergePythonSpecExtras(existingSpec, requestedSpec string) string {
	// 把一个写法拆成：基础名、extras（逐个原样）、extras 之后剩下的部分。
	// 基础名只认 PEP 508 包名字符；基础名和 `[` 之间允许有空白（`httpx [http2]` 也是合法写法）。
	split := func(spec string) (base string, extras []string, rest string) {
		spec = strings.TrimSpace(spec)
		i := 0
		for i < len(spec) {
			ch := spec[i]
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == '_' {
				i++
				continue
			}
			break
		}
		base = spec[:i]
		rest = spec[i:]
		afterSpace := strings.TrimLeft(rest, " \t")
		if !strings.HasPrefix(afterSpace, "[") {
			// 没有 extras：基础名之后整段都是 rest（如 `; python_version >= "3.8"`），前导空白原样保留。
			return base, nil, rest
		}
		end := strings.IndexByte(afterSpace, ']')
		if end < 0 {
			// 方括号没闭合：PythonExtrasCovered 已按「没有 extras」处理，这里同样不认 extras，
			// 后面那截残缺内容 pip 也解析不了，直接丢掉，不再带进新名字。
			return base, nil, ""
		}
		for _, extra := range strings.Split(afterSpace[1:end], ",") {
			if extra = strings.TrimSpace(extra); extra != "" {
				extras = append(extras, extra)
			}
		}
		return base, extras, afterSpace[end+1:]
	}

	_, oldExtras, oldRest := split(existingSpec)
	newBase, newExtras, newRest := split(requestedSpec)
	if newBase == "" {
		// 本次写法连包名都拆不出来（调用方已按归一化键确认过是同一个包，正常走不到），原样返回本次写法兜底。
		return strings.TrimSpace(requestedSpec)
	}

	// extras 去重口径与 PythonExtrasCovered 一致：小写 + PEP 503 分隔符折叠。
	seen := map[string]bool{}
	merged := make([]string, 0, len(oldExtras)+len(newExtras))
	for _, extra := range append(append([]string{}, oldExtras...), newExtras...) {
		key := pep503SeparatorPattern.ReplaceAllString(strings.ToLower(extra), "-")
		if seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, extra)
	}

	rest := oldRest
	if strings.TrimSpace(newRest) != "" {
		rest = newRest
	}

	result := newBase
	if len(merged) > 0 {
		result += "[" + strings.Join(merged, ",") + "]"
	}
	return result + rest
}

// stripPythonVersionSpecifier 在第一个版本算符 / 空白 / 括号 / 逗号处截断，仅保留包名部分。
func stripPythonVersionSpecifier(name string) string {
	for i := 0; i < len(name); i++ {
		switch name[i] {
		case '=', '<', '>', '!', '~', ' ', '\t', '\r', '\n', '(', ',':
			return name[:i]
		}
	}
	return name
}

// pythonDependencyStatusMergePriority 给依赖状态打优先级，用于存量合并时挑选保留行。
// installed > installing/queued > failed/其它。
func pythonDependencyStatusMergePriority(status string) int {
	switch status {
	case model.DepStatusInstalled:
		return 3
	case model.DepStatusInstalling, model.DepStatusQueued:
		return 2
	default:
		return 1
	}
}

// pythonDependencyMergePrefers 判断 candidate 是否应取代 current 成为保留行。
// 先比状态优先级，再比最近 updated_at，最后比 ID（取较大）。
func pythonDependencyMergePrefers(candidate, current model.Dependency) bool {
	cp := pythonDependencyStatusMergePriority(candidate.Status)
	wp := pythonDependencyStatusMergePriority(current.Status)
	if cp != wp {
		return cp > wp
	}
	if !candidate.UpdatedAt.Equal(current.UpdatedAt) {
		return candidate.UpdatedAt.After(current.UpdatedAt)
	}
	return candidate.ID > current.ID
}

// FindExistingPythonDependency 在 Python 依赖里按归一化键 + python 版本查找已存在记录。
// statuses 为空表示不限状态。返回命中的第一条记录。
func FindExistingPythonDependency(name, pythonVersion string, statuses ...string) (model.Dependency, bool) {
	canonical := CanonicalizePythonPackageName(name)
	if canonical == "" {
		return model.Dependency{}, false
	}

	query := database.DB.
		Where("type = ?", model.DepTypePython).
		Where("COALESCE(NULLIF(python_version, ''), ?) = ?", LegacyPythonVersion(), pythonVersion)
	if len(statuses) > 0 {
		query = query.Where("status IN ?", statuses)
	}

	var candidates []model.Dependency
	if err := query.Find(&candidates).Error; err != nil {
		return model.Dependency{}, false
	}
	for _, candidate := range candidates {
		if CanonicalizePythonPackageName(candidate.Name) == canonical {
			return candidate, true
		}
	}
	return model.Dependency{}, false
}

// MergeDuplicatePythonDependencies 在启动期一次性合并 Python 依赖的重复行。
//
// 按（归一化 python 版本, PEP 503 归一化键）分组，组内多行只保留状态优先级最高 / 最新一条，
// 删除其余行。幂等：重复执行无副作用（每组只剩一行后不再删除）。
func MergeDuplicatePythonDependencies() error {
	if database.DB == nil {
		return nil
	}

	var deps []model.Dependency
	if err := database.DB.Where("type = ?", model.DepTypePython).Find(&deps).Error; err != nil {
		return fmt.Errorf("load python dependencies for dedup merge: %w", err)
	}
	if len(deps) <= 1 {
		return nil
	}

	groups := make(map[string][]model.Dependency)
	for _, dep := range deps {
		// 名称无法解析出归一化键（空名/纯版本算符等异常行）一律跳过，绝不参与合并删除，避免误删。
		canonical := CanonicalizePythonPackageName(dep.Name)
		if canonical == "" {
			continue
		}
		version := NormalizeDependencyPythonVersion(dep.PythonVersion)
		key := version + "\x00" + canonical
		groups[key] = append(groups[key], dep)
	}

	var removeIDs []uint
	for _, group := range groups {
		if len(group) <= 1 {
			continue
		}
		winner := group[0]
		for _, candidate := range group[1:] {
			if pythonDependencyMergePrefers(candidate, winner) {
				winner = candidate
			}
		}
		for _, dep := range group {
			if dep.ID != winner.ID {
				removeIDs = append(removeIDs, dep.ID)
			}
		}
	}

	if len(removeIDs) == 0 {
		return nil
	}
	if err := database.DB.Where("id IN ?", removeIDs).Delete(&model.Dependency{}).Error; err != nil {
		return fmt.Errorf("remove duplicate python dependencies: %w", err)
	}
	log.Printf("info: merged duplicate python dependencies, removed %d redundant rows", len(removeIDs))
	return nil
}
