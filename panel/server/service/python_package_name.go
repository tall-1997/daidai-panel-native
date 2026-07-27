package service

import (
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
// 删除其余行。幂等：重复执行无副作用（每组只剩一行后不再删除）。失败只记日志不 panic。
func MergeDuplicatePythonDependencies() {
	if database.DB == nil {
		return
	}

	var deps []model.Dependency
	if err := database.DB.Where("type = ?", model.DepTypePython).Find(&deps).Error; err != nil {
		log.Printf("warn: failed to load python dependencies for dedup merge: %v", err)
		return
	}
	if len(deps) <= 1 {
		return
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
		return
	}
	if err := database.DB.Where("id IN ?", removeIDs).Delete(&model.Dependency{}).Error; err != nil {
		log.Printf("warn: failed to remove duplicate python dependencies: %v", err)
		return
	}
	log.Printf("info: merged duplicate python dependencies, removed %d redundant rows", len(removeIDs))
}
