package tracker

import (
	"context"
	"fmt"
	"io"
	"os"
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
	expiresAt       time.Time     // Token expiration time
	tokenLifetime   time.Duration // Token lifetime (12 hours for IAM tokens)
	refreshInterval time.Duration
	cliCommand      string
	initCommand     string
	federationID    string
	logger          *zap.Logger
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewTokenManager creates a new token manager
func NewTokenManager(refreshInterval time.Duration, cliCommand string, initCommand string, federationID string, logger *zap.Logger) *TokenManager {
	ctx, cancel := context.WithCancel(context.Background())

	tm := &TokenManager{
		tokenLifetime:   12 * time.Hour, // IAM tokens live up to 12 hours
		refreshInterval: refreshInterval,
		cliCommand:      cliCommand,
		initCommand:     initCommand,
		federationID:    federationID,
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
func (tm *TokenManager) ycExecutable() string {
	parts := strings.Fields(tm.cliCommand)
	if len(parts) == 0 {
		return "yc"
	}
	return parts[0]
}

func (tm *TokenManager) checkYCAuth() error {
	// Try to get current config (non-interactive check)
	cmd := exec.Command(tm.ycExecutable(), "config", "list")
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

// ensureYCAuth verifies authentication and attempts automatic yc init if needed
func (tm *TokenManager) ensureYCAuth() error {
	if err := tm.checkYCAuth(); err == nil {
		return nil
	}

	tm.logger.Warn("yc CLI not authenticated, running 'yc init' automatically")

	if err := tm.runYCInit(); err != nil {
		return fmt.Errorf("authentication check failed and automatic 'yc init' failed: %w", err)
	}

	// Re-check after init
	if err := tm.checkYCAuth(); err != nil {
		return fmt.Errorf("authentication check still failing after 'yc init': %w", err)
	}

	return nil
}

// runYCInit launches interactive yc init so user can complete auth
func (tm *TokenManager) runYCInit() error {
	var cmd *exec.Cmd
	if tm.initCommand != "" {
		initParts := strings.Fields(tm.initCommand)
		if len(initParts) == 0 {
			return fmt.Errorf("init command is empty")
		}
		cmd = exec.Command(initParts[0], initParts[1:]...)
	} else {
		args := []string{"init"}
		if tm.federationID != "" {
			args = append(args, "--federation-id", tm.federationID)
		}
		cmd = exec.Command(tm.ycExecutable(), args...)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Automatically answer "1" (re-initialize default profile), then pass through user input
	autoAnswer := strings.NewReader("1\n")
	cmd.Stdin = io.MultiReader(autoAnswer, os.Stdin)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("yc init command failed: %w", err)
	}

	return nil
}

// getIAMToken executes yc CLI command to get IAM token
func (tm *TokenManager) getIAMToken() (string, error) {
	token, err := tm.tryGetIAMToken()
	if err == nil {
		return token, nil
	}

	if tm.isAuthError(err) {
		tm.logger.Warn("yc CLI authentication failed, attempting automatic init", zap.Error(err))
		if initErr := tm.ensureYCAuth(); initErr != nil {
			return "", fmt.Errorf("authentication check failed and automatic 'yc init' failed: %w", initErr)
		}
		return tm.tryGetIAMToken()
	}

	return "", err
	}

// GetLastRefreshTime returns the last time token was refreshed
func (tm *TokenManager) GetLastRefreshTime() time.Time {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.lastRefresh
}

func (tm *TokenManager) tryGetIAMToken() (string, error) {
	parts := strings.Fields(tm.cliCommand)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty CLI command")
	}

	cmd := exec.Command(parts[0], parts[1:]...)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderrMsg := string(exitErr.Stderr)
			if strings.Contains(stderrMsg, "not authenticated") ||
				strings.Contains(stderrMsg, "authentication") ||
				strings.Contains(stderrMsg, "OAuth token") {
				return "", fmt.Errorf("yc CLI authentication expired: %s", stderrMsg)
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

func (tm *TokenManager) isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "authentication") ||
		strings.Contains(msg, "OAuth token") ||
		strings.Contains(msg, "not authenticated")
}
