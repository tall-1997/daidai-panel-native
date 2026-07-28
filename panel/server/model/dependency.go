package model

import (
	"encoding/json"
	"time"
)

type Dependency struct {
	ID                   uint      `json:"id" gorm:"primaryKey"`
	Type                 string    `json:"type" gorm:"type:varchar(20);not null;index"`
	Name                 string    `json:"name" gorm:"type:varchar(255);not null"`
	PythonVersion        string    `json:"python_version" gorm:"type:varchar(16);default:'';index"`
	Status               string    `json:"status" gorm:"type:varchar(20);default:installing"`
	Log                  string    `json:"-" gorm:"type:text"`
	OperationID          string    `json:"operation_id,omitempty" gorm:"type:varchar(64);default:'';index"`
	CompatibilityDetails string    `json:"-" gorm:"type:text"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (Dependency) TableName() string {
	return "dependencies"
}

func (d *Dependency) ToDict() map[string]interface{} {
	m := map[string]interface{}{
		"id":             d.ID,
		"type":           d.Type,
		"name":           d.Name,
		"python_version": d.PythonVersion,
		"status":         d.Status,
		"operation_id":   d.OperationID,
		"created_at":     d.CreatedAt,
		"updated_at":     d.UpdatedAt,
	}
	if d.CompatibilityDetails != "" {
		var details map[string]interface{}
		if err := json.Unmarshal([]byte(d.CompatibilityDetails), &details); err == nil {
			m["compatibility_details"] = details
		} else {
			m["compatibility_details"] = d.CompatibilityDetails
		}
	}
	return m
}

func (d *Dependency) ToDictWithLog() map[string]interface{} {
	m := d.ToDict()
	m["log"] = d.Log
	return m
}

const (
	DepTypeNodeJS       = "nodejs"
	DepTypePython       = "python"
	DepTypeLinux        = "linux"
	DepStatusQueued     = "queued"
	DepStatusInstalling = "installing"
	DepStatusInstalled  = "installed"
	DepStatusFailed     = "failed"
	DepStatusRemoving   = "removing"
	DepStatusCancelled  = "cancelled"
)
