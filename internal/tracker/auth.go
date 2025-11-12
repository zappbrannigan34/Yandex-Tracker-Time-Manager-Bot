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
	mu              sync.RWMutex
	token           string
	lastRefresh     time.Time
	expiresAt       time.Time // Token expiration time
	tokenLifetime   time.Duration // Token lifetime (12 hours for IAM tokens)
	refreshInterval time.Duration
	cliCommand      string
	logger          *zap.Logger
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewTokenManager creates a new token manager
func NewTokenManager(refreshInterval time.Duration, cliCommand string, logger *zap.Logger) *TokenManager {
	ctx, cancel := context.WithCancel(context.Background())

	tm := &TokenManager{
		tokenLifetime:   12 * time.Hour, // IAM tokens live up to 12 hours
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

// IsTokenValid checks if current token is still valid
// Token is considered valid if it has more than 1 hour until expiration
func (tm *TokenManager) IsTokenValid() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tm.token == "" {
		return false
	}

	// Check if token expires in less than 1 hour
	timeUntilExpiry := time.Until(tm.expiresAt)
	return timeUntilExpiry > time.Hour
}

// Refresh refreshes the IAM token
func (tm *TokenManager) Refresh() error {
	// Check if token is still valid
	if tm.IsTokenValid() {
		tm.logger.Debug("Token is still valid, skipping refresh",
			zap.Time("expires_at", tm.expiresAt),
			zap.Duration("time_until_expiry", time.Until(tm.expiresAt)))
		return nil
	}

	token, err := tm.getIAMToken()
	if err != nil {
		tm.logger.Error("Failed to refresh IAM token", zap.Error(err))

		// If we have an existing token, keep using it even if expired
		// This allows daemon to continue working if yc CLI requires re-auth
		tm.mu.RLock()
		hasExistingToken := tm.token != ""
		tm.mu.RUnlock()

		if hasExistingToken {
			tm.logger.Warn("Continuing with existing token despite refresh failure",
				zap.String("hint", "Run 'yc init' to re-authenticate if needed"))
			// Don't return error - allow daemon to continue
			return nil
		}

		return err
	}

	now := time.Now()
	expiresAt := now.Add(tm.tokenLifetime)

	tm.mu.Lock()
	tm.token = token
	tm.lastRefresh = now
	tm.expiresAt = expiresAt
	tm.mu.Unlock()

	tm.logger.Info("IAM token refreshed successfully",
		zap.Time("last_refresh", now),
		zap.Time("expires_at", expiresAt),
		zap.Duration("lifetime", tm.tokenLifetime))

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

// checkYCAuth checks if yc CLI is authenticated
func (tm *TokenManager) checkYCAuth() error {
	// Try to get current config (non-interactive check)
	cmd := exec.Command("yc", "config", "list")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("yc CLI not configured or not authenticated")
	}

	// Check if output contains required fields
	outputStr := string(output)
	if !strings.Contains(outputStr, "token:") && !strings.Contains(outputStr, "service-account-key:") {
		return fmt.Errorf("yc CLI authenticated but no credentials found")
	}

	return nil
}

// getIAMToken executes yc CLI command to get IAM token
func (tm *TokenManager) getIAMToken() (string, error) {
	// Check yc auth status first (non-interactive)
	if err := tm.checkYCAuth(); err != nil {
		return "", fmt.Errorf("authentication check failed: %w (hint: run 'yc init' to re-authenticate)", err)
	}

	// Parse command (e.g., "yc iam create-token")
	parts := strings.Fields(tm.cliCommand)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty CLI command")
	}

	cmd := exec.Command(parts[0], parts[1:]...)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderrMsg := string(exitErr.Stderr)
			// Check if it's an auth error
			if strings.Contains(stderrMsg, "not authenticated") ||
			   strings.Contains(stderrMsg, "authentication") {
				return "", fmt.Errorf("yc CLI authentication expired: %s (hint: run 'yc init')", stderrMsg)
			}
			return "", fmt.Errorf("yc CLI failed: %s: %s", err, stderrMsg)
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
