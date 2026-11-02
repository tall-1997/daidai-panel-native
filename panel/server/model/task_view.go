package model

import "time"

type TaskView struct {
	ID uint `gorm:"primarykey" json:"id"`
	// 任务视图是全局的（这张表没有 user_id，ListViews 也不按用户过滤），所以唯一键就是裸 name。
	// 视图名是标签页文案，同名两个页签用户根本分不清，连点创建按钮更是直接出现两个一样的标签。
	// 老库里的同名残留由 database.DeduplicateBeforeUniqueIndex() 在 AutoMigrate 之前改名让路。
	Name      string    `gorm:"size:128;uniqueIndex;not null" json:"name"`
	Filters   string    `gorm:"type:text;default:'[]'" json:"filters"`
	SortRules string    `gorm:"type:text;default:'[]'" json:"sort_rules"`
	Hidden    bool      `gorm:"default:false;index" json:"hidden"`
	SortOrder int       `gorm:"default:0;index" json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (TaskView) TableName() string {
	return "task_views"
}
