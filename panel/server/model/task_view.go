package model

import "time"

type TaskView struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Name      string    `gorm:"size:128;not null" json:"name"`
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
