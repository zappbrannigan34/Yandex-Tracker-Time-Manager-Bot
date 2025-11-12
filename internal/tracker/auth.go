package tracker

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// TokenManager manages IAM token lifecycle
type TokenManager struct {
	mu            sync.RWMutex
	token         string
	lastRefresh   time.Time
	refreshInterval time.Duration
	cliCommand    string
	logger        *zap.Logger
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewTokenManager creates a new token manager
func NewTokenManager(refreshInterval time.Duration, cliCommand string, logger *zap.Logger) *TokenManager {
	ctx, cancel := context.WithCancel(context.Background())

	tm := &TokenManager{
		refreshInterval: refreshInterval,
		cliCommand:      cliCommand,
		logger:          logger,
		ctx:             ctx,
		cancel:          cancel,
	}

	return tm
}

// Start starts automatic token refresh
func (tm *TokenManager) Start() error {
	// Get initial token
	if err := tm.Refresh(); err != nil {
		return fmt.Errorf("failed to get initial token: %w", err)
	}

	// Start refresh goroutine
	go tm.refreshLoop()

	tm.logger.Info("Token manager started",
		zap.Duration("refresh_interval", tm.refreshInterval))

	return nil
}

// Stop stops the token manager
func (tm *TokenManager) Stop() {
	tm.cancel()
	tm.logger.Info("Token manager stopped")
}

// GetToken returns current valid token
func (tm *TokenManager) GetToken() (string, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tm.token == "" {
		return "", fmt.Errorf("token not available")
	}

	return tm.token, nil
}

// Refresh refreshes the IAM token
func (tm *TokenManager) Refresh() error {
	token, err := tm.getIAMToken()
	if err != nil {
		tm.logger.Error("Failed to refresh IAM token", zap.Error(err))
		return err
	}

	tm.mu.Lock()
	tm.token = token
	tm.lastRefresh = time.Now()
	tm.mu.Unlock()

	tm.logger.Info("IAM token refreshed successfully",
		zap.Time("last_refresh", tm.lastRefresh))

	return nil
}

// refreshLoop periodically refreshes the token
func (tm *TokenManager) refreshLoop() {
	ticker := time.NewTicker(tm.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-ticker.C:
			if err := tm.Refresh(); err != nil {
				tm.logger.Error("Failed to refresh token in background",
					zap.Error(err))
				// Continue trying - don't stop the loop
			}
		}
	}
}

// getIAMToken executes yc CLI command to get IAM token
func (tm *TokenManager) getIAMToken() (string, error) {
	// Parse command (e.g., "yc iam create-token")
	parts := strings.Fields(tm.cliCommand)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty CLI command")
	}

	cmd := exec.Command(parts[0], parts[1:]...)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("yc CLI failed: %s: %s", err, string(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to execute yc CLI: %w", err)
	}

	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", fmt.Errorf("empty token received from yc CLI")
	}

	return token, nil
}

// GetLastRefreshTime returns the last time token was refreshed
func (tm *TokenManager) GetLastRefreshTime() time.Time {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.lastRefresh
}
