package model

import "time"

type Dependency struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	Type          string    `json:"type" gorm:"type:varchar(20);not null;index"`
	Name          string    `json:"name" gorm:"type:varchar(255);not null"`
	PythonVersion string    `json:"python_version" gorm:"type:varchar(16);default:'';index"`
	Status        string    `json:"status" gorm:"type:varchar(20);default:installing"`
	Log           string    `json:"-" gorm:"type:text"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (Dependency) TableName() string {
	return "dependencies"
}

func (d *Dependency) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":             d.ID,
		"type":           d.Type,
		"name":           d.Name,
		"python_version": d.PythonVersion,
		"status":         d.Status,
		"created_at":     d.CreatedAt,
		"updated_at":     d.UpdatedAt,
	}
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
