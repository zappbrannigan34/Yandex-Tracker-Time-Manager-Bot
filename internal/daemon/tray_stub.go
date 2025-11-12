// +build !windows

package daemon

import (
	"errors"

	"go.uber.org/zap"
)

// TrayApp represents system tray application (stub for non-Windows platforms)
type TrayApp struct {
	logger *zap.Logger
}

// NewTrayApp creates a new system tray application (not supported on this platform)
func NewTrayApp(daemon *Daemon, logger *zap.Logger) (*TrayApp, error) {
	return nil, errors.New("system tray is only supported on Windows")
}

// Run does nothing on non-Windows platforms
func (t *TrayApp) Run() {
}

// Stop does nothing on non-Windows platforms
func (t *TrayApp) Stop() {
}

// ShowNotification does nothing on non-Windows platforms
func (t *TrayApp) ShowNotification(title, message string) {
}
