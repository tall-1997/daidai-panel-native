package service

import (
	"os"
	"sync"
	"time"

	"daidai-panel/model"
)

var panelTimezoneState = struct {
	sync.RWMutex
	name string
}{
	name: model.DefaultPanelTimezone,
}

type PanelTimezoneState struct {
	Location *time.Location
	Name     string
	TZ       string
	TZSet    bool
}

func ApplyPanelTimezone(value string) error {
	normalized, err := model.NormalizeSystemConfigValue(model.PanelTimezoneConfigKey, value)
	if err != nil {
		return err
	}

	location, err := time.LoadLocation(normalized)
	if err != nil {
		return err
	}

	// Go 进程内的 time.Now() 使用 time.Local；子进程和脚本再通过 TZ 继承同一个面板时区。
	time.Local = location
	_ = os.Setenv("TZ", normalized)

	panelTimezoneState.Lock()
	panelTimezoneState.name = normalized
	panelTimezoneState.Unlock()
	return nil
}

func ApplyRegisteredPanelTimezone() error {
	return ApplyPanelTimezone(model.GetRegisteredConfig(model.PanelTimezoneConfigKey))
}

func CurrentPanelTimezone() string {
	panelTimezoneState.RLock()
	name := panelTimezoneState.name
	panelTimezoneState.RUnlock()

	if name == "" {
		return model.DefaultPanelTimezone
	}
	return name
}

func CapturePanelTimezoneState() PanelTimezoneState {
	panelTimezoneState.RLock()
	defer panelTimezoneState.RUnlock()
	tz, exists := os.LookupEnv("TZ")
	return PanelTimezoneState{
		Location: time.Local,
		Name:     panelTimezoneState.name,
		TZ:       tz,
		TZSet:    exists,
	}
}

func RestorePanelTimezoneState(state PanelTimezoneState) error {
	if state.Location == nil {
		state.Location = time.UTC
	}
	time.Local = state.Location
	if state.TZSet {
		if err := os.Setenv("TZ", state.TZ); err != nil {
			return err
		}
	} else if err := os.Unsetenv("TZ"); err != nil {
		return err
	}
	panelTimezoneState.Lock()
	panelTimezoneState.name = state.Name
	panelTimezoneState.Unlock()
	return nil
}
