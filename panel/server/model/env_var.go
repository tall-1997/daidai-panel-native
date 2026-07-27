package model

import (
	"strings"
	"time"
)

type EnvVar struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Name      string    `gorm:"size:128;index;not null" json:"name"`
	Value     string    `gorm:"type:text;default:''" json:"value"`
	Remarks   string    `gorm:"size:256;default:''" json:"remarks"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	Position  float64   `gorm:"default:10000.0;index" json:"position"`
	SortOrder int       `gorm:"default:0" json:"sort_order"`
	Group     string    `gorm:"size:512;default:'';index" json:"group"`
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
		"remarks":    e.Remarks,
		"enabled":    e.Enabled,
		"position":   e.Position,
		"sort_order": e.SortOrder,
		"group":      e.Group,
		"groups":     SplitEnvGroups(e.Group),
		"created_at": e.CreatedAt,
		"updated_at": e.UpdatedAt,
	}
}
