package service

import (
	"os"
	"sync"
	"time"

	"daidai-panel/model"
)

var panelTimezoneState = struct {
	sync.RWMutex
	name     string
	location *time.Location
}{
	name:     model.DefaultPanelTimezone,
	location: time.Local,
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

	panelTimezoneState.Lock()
	// time.Local is a process-global pointer read implicitly by time.Now and formatters.
	// Mutating it while workers run creates an unavoidable race, so runtime code uses
	// this guarded location and passes TZ explicitly to child processes.
	panelTimezoneState.name = normalized
	panelTimezoneState.location = location
	panelTimezoneState.Unlock()
	_ = os.Setenv("TZ", normalized)
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
		Location: panelTimezoneState.location,
		Name:     panelTimezoneState.name,
		TZ:       tz,
		TZSet:    exists,
	}
}

func RestorePanelTimezoneState(state PanelTimezoneState) error {
	if state.Location == nil {
		state.Location = time.UTC
	}
	panelTimezoneState.Lock()
	panelTimezoneState.name = state.Name
	panelTimezoneState.location = state.Location
	panelTimezoneState.Unlock()
	if state.TZSet {
		if err := os.Setenv("TZ", state.TZ); err != nil {
			return err
		}
	} else if err := os.Unsetenv("TZ"); err != nil {
		return err
	}
	return nil
}
