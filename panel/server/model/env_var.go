package model

import (
	"strings"
	"time"
)

type EnvVar struct {
	ID uint `gorm:"primarykey" json:"id"`
	// 🔴 这里的 index 是普通索引，绝对不要「顺手补齐」成 uniqueIndex。
	// 同名多条是青龙风格的刻意功能（多账号场景）：新建一律纯 insert，运行时由
	// BuildManagedRuntimeEnvMap 按 name 分组再用 & 拼接暴露给脚本，前端还有「复制同名变量」入口。
	// database.dropEnvVarUniqueIndex() 每次启动都会主动 DROP 掉历史遗留的唯一索引，
	// 在这里加唯一键既会废掉多账号能力，也会和那段启动清理逻辑互相拆台。
	Name      string    `gorm:"size:128;index;not null" json:"name"`
	Value     string    `gorm:"type:text;default:''" json:"value"`
	Secret    bool      `gorm:"default:false" json:"secret"`
	Sealed    string    `gorm:"type:text;default:''" json:"-"`
	Remarks   string    `gorm:"size:256;default:''" json:"remarks"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	Position  float64   `gorm:"default:10000;index" json:"position"` // 只能写 10000：写成 10000.0 与 DDL 对不上，每次 AutoMigrate 都整表重建（#156，见 database.go 补列处）
	SortOrder int       `gorm:"default:0" json:"sort_order"`
	Group     string    `gorm:"size:512;default:'';index" json:"group"`
	Important bool      `gorm:"default:false" json:"important"` // 「重要」标记（APP #16）：只给客户端做醒目显示与删除前确认，服务端不拦删除；ToDict 永远带它，别加 omitempty
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (EnvVar) TableName() string {
	return "env_vars"
}

func SplitEnvGroups(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；' || r == '\n' || r == '\r' || r == '\t'
	})

	groups := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		group := strings.TrimSpace(field)
		if group == "" {
			continue
		}
		if _, exists := seen[group]; exists {
			continue
		}
		seen[group] = struct{}{}
		groups = append(groups, group)
	}
	return groups
}

func JoinEnvGroups(groups []string) string {
	normalized := make([]string, 0, len(groups))
	seen := make(map[string]struct{}, len(groups))
	for _, raw := range groups {
		for _, group := range SplitEnvGroups(raw) {
			if _, exists := seen[group]; exists {
				continue
			}
			seen[group] = struct{}{}
			normalized = append(normalized, group)
		}
	}
	return strings.Join(normalized, ",")
}

func NormalizeEnvGroupValue(value string) string {
	return JoinEnvGroups([]string{value})
}

func (e *EnvVar) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":         e.ID,
		"name":       e.Name,
		"value":      e.Value,
		"secret":     e.Secret,
		"remarks":    e.Remarks,
		"enabled":    e.Enabled,
		"position":   e.Position,
		"sort_order": e.SortOrder,
		"group":      e.Group,
		"groups":     SplitEnvGroups(e.Group),
		"important":  e.Important,
		"created_at": e.CreatedAt,
		"updated_at": e.UpdatedAt,
	}
}
