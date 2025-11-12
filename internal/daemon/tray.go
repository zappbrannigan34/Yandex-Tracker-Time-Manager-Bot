// +build windows

package daemon

import (
	"fmt"
	"syscall"
	"unsafe"

	"fyne.io/systray"
	"go.uber.org/zap"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	messageBoxW      = user32.NewProc("MessageBoxW")
)

const (
	MB_OK                = 0x00000000
	MB_ICONINFORMATION   = 0x00000040
)

// TrayApp represents system tray application
type TrayApp struct {
	daemon *Daemon
	logger *zap.Logger
	quit   chan struct{}
}

// NewTrayApp creates a new system tray application
func NewTrayApp(daemon *Daemon, logger *zap.Logger) (*TrayApp, error) {
	return &TrayApp{
		daemon: daemon,
		logger: logger,
		quit:   make(chan struct{}),
	}, nil
}

// Run starts the system tray application (blocks until Quit)
func (t *TrayApp) Run() {
	systray.Run(t.onReady, t.onExit)
}

func (t *TrayApp) onReady() {
	// Set icon from file or embedded
	iconData := getClockIcon()
	systray.SetIcon(iconData)
	systray.SetTitle("TT")
	systray.SetTooltip("Yandex Tracker Time Manager")

	// Add menu items
	mSyncNow := systray.AddMenuItem("Sync Now", "Run time sync immediately")
	systray.AddSeparator()
	mStatus := systray.AddMenuItem("Status", "Show current status")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Exit the application")

	// Start daemon logic in background
	go t.daemon.runScheduledLogic()

	// Handle menu item clicks
	go func() {
		for {
			select {
			case <-mSyncNow.ClickedCh:
				t.logger.Info("Sync Now clicked from tray")
				go t.daemon.SyncNow()
			case <-mStatus.ClickedCh:
				t.logger.Info("Status clicked from tray")
				t.showStatus()
			case <-mQuit.ClickedCh:
				t.logger.Info("Quit clicked from tray")
				t.daemon.Stop()
				systray.Quit()
				return
			case <-t.quit:
				systray.Quit()
				return
			}
		}
	}()
}

func (t *TrayApp) onExit() {
	t.logger.Info("System tray exited")
}

// Stop stops the system tray application
func (t *TrayApp) Stop() {
	close(t.quit)
}

// ShowNotification shows a notification (Windows only)
func (t *TrayApp) ShowNotification(title, message string) {
	// fyne.io/systray doesn't have built-in notification support
	// Just log for now
	t.logger.Info("Notification", zap.String("title", title), zap.String("message", message))
}

// showStatus shows current tracking status
func (t *TrayApp) showStatus() {
	status := t.daemon.GetStatus()
	t.logger.Info("Current status", zap.Any("status", status))

	// Format status message
	var message string
	if todayData, ok := status["today"].(map[string]interface{}); ok {
		message = fmt.Sprintf(
			"Date: %v\nWorked: %.0f min\nTarget: %.0f min\nProgress: %.1f%%",
			todayData["date"],
			todayData["worked_minutes"],
			todayData["target_minutes"],
			todayData["progress_percent"],
		)
		systray.SetTooltip(message)
	} else {
		message = "No status available"
	}

	// Show MessageBox with status
	showMessageBox("Time Tracker Status", message)
}

func showMessageBox(title, message string) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messagePtr, _ := syscall.UTF16PtrFromString(message)
	messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(MB_OK|MB_ICONINFORMATION),
	)
}
