package tracker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	defaultTimeout = 30 * time.Second
	defaultRetries = 3
)

// Client represents Yandex Tracker API client
type Client struct {
	baseURL      string
	orgID        string
	tokenManager *TokenManager
	httpClient   *http.Client
	logger       *zap.Logger
	currentUser  *User // Cached current user info
}

// NewClient creates a new Tracker API client
func NewClient(baseURL, orgID string, tokenManager *TokenManager, logger *zap.Logger) *Client {
	return &Client{
		baseURL:      baseURL,
		orgID:        orgID,
		tokenManager: tokenManager,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
	}
}

// SearchIssues searches for issues using query language
func (c *Client) SearchIssues(query string) ([]Issue, error) {
	req := SearchIssuesRequest{
		Query: query,
	}

	var issues []Issue
	err := c.doRequest("POST", "/v2/issues/_search", req, &issues)
	if err != nil {
		return nil, fmt.Errorf("failed to search issues: %w", err)
	}

	c.logger.Info("Issues found",
		zap.String("query", query),
		zap.Int("count", len(issues)))

	return issues, nil
}

// GetCurrentUser returns current authenticated user info (cached)
func (c *Client) GetCurrentUser() (*User, error) {
	if c.currentUser != nil {
		return c.currentUser, nil
	}

	var user User
	err := c.doRequest("GET", "/v2/myself", nil, &user)
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	c.currentUser = &user

	// If ID is empty, extract from Self URL
	if user.ID == "" && user.Self != "" {
		// Extract ID from URL like: https://api.tracker.yandex.net/v2/users/1234567890123456
		parts := strings.Split(user.Self, "/")
		if len(parts) > 0 {
			extractedID := parts[len(parts)-1]
			user.ID = FlexibleID(extractedID)
			c.logger.Info("Extracted user ID from Self URL",
				zap.String("id", extractedID))
		}
	}

	c.logger.Info("Current user identified",
		zap.String("display", user.Display),
		zap.String("id", user.ID.String()),
		zap.String("self", user.Self))

	return &user, nil
}

// GetWorklogsForToday gets all worklogs for current user for today
func (c *Client) GetWorklogsForToday(date time.Time) ([]Worklog, error) {
	// IMPORTANT: Yandex Tracker API only supports filtering by createdAt (when worklog was logged),
	// NOT by start (when work was actually performed). Users often backfill time entries.
	// Solution: Fetch worklogs created in a window around target date, then filter client-side by start date.

	// Get current user for filtering
	currentUser, err := c.GetCurrentUser()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	// Fetch worklogs created from start of month to end of month + 7 days
	// This catches: (1) worklogs for target date, (2) backfilled entries created later
	// With createdBy filter, API should return only user's worklogs
	startOfMonth := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, time.Local)
	endOfMonth := time.Date(date.Year(), date.Month()+1, 0, 23, 59, 59, 999, time.Local)
	createdFrom := startOfMonth
	createdTo := endOfMonth.AddDate(0, 0, 7) // +7 days after month end

	req := SearchWorklogsRequest{
		CreatedBy: currentUser.ID.String(), // Filter by user ID (extracted from Self URL)
		CreatedAt: &TimeRange{
			From: createdFrom.Format("2006-01-02T15:04:05.000-0700"),
			To:   createdTo.Format("2006-01-02T15:04:05.000-0700"),
		},
	}

	c.logger.Info("Searching worklogs",
		zap.Time("created_from", createdFrom),
		zap.Time("created_to", createdTo),
		zap.Time("target_date", date))

	var allWorklogs []Worklog
	err = c.doRequest("POST", "/v2/worklog/_search", req, &allWorklogs)
	if err != nil {
		return nil, fmt.Errorf("failed to get worklogs: %w", err)
	}

	// Filter client-side by start date (when work was actually performed) AND current user
	var worklogs []Worklog

	for _, wl := range allWorklogs {
		// Convert worklog start time to local timezone for date comparison
		startLocal := wl.Start.In(time.Local)

		// Check if worklog belongs to current user
		// If ID is empty, fallback to Display name comparison
		var isCurrentUser bool
		if currentUser.ID != "" {
			isCurrentUser = wl.CreatedBy.ID == currentUser.ID
		} else {
			isCurrentUser = wl.CreatedBy.Display == currentUser.Display
		}

		// Check if worklog's start time falls within target day
		if startLocal.Year() == date.Year() &&
			startLocal.Month() == date.Month() &&
			startLocal.Day() == date.Day() &&
			isCurrentUser {
			worklogs = append(worklogs, wl)

			// Parse duration to minutes for readable logging
			minutes, _ := ParseISO8601Duration(wl.Duration)
			hours := int(minutes / 60)
			mins := int(minutes) % 60

			c.logger.Info("Worklog found",
				zap.String("issue", wl.Issue.Key),
				zap.String("duration", wl.Duration),
				zap.String("time", fmt.Sprintf("%dh %dm", hours, mins)),
				zap.Time("start", startLocal),
				zap.String("comment", wl.Comment))
		}
	}

	c.logger.Info("Worklogs retrieved and filtered",
		zap.Time("date", date),
		zap.Int("total_fetched", len(allWorklogs)),
		zap.Int("filtered_count", len(worklogs)),
		zap.String("target_day", date.Format("2006-01-02")))

	return worklogs, nil
}

// GetWorklogsForRange gets all worklogs for current user for date range
func (c *Client) GetWorklogsForRange(from, to time.Time) ([]Worklog, error) {
	// Get current user for filtering
	currentUser, err := c.GetCurrentUser()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	// Expand search window to catch backfilled entries
	// Search from start of 'from' month to end of 'to' month + 7 days
	startOfMonth := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.Local)
	endOfMonth := time.Date(to.Year(), to.Month()+1, 0, 23, 59, 59, 999, time.Local)
	createdFrom := startOfMonth
	createdTo := endOfMonth.AddDate(0, 0, 7) // +7 days buffer

	req := SearchWorklogsRequest{
		CreatedBy: currentUser.ID.String(),
		CreatedAt: &TimeRange{
			From: createdFrom.Format("2006-01-02T15:04:05.000-0700"),
			To:   createdTo.Format("2006-01-02T15:04:05.000-0700"),
		},
	}

	c.logger.Info("Searching worklogs for range",
		zap.Time("from", from),
		zap.Time("to", to),
		zap.Time("created_from", createdFrom),
		zap.Time("created_to", createdTo))

	var allWorklogs []Worklog
	err = c.doRequest("POST", "/v2/worklog/_search", req, &allWorklogs)
	if err != nil {
		return nil, fmt.Errorf("failed to get worklogs: %w", err)
	}

	// Filter client-side by start date range AND current user
	var worklogs []Worklog

	for _, wl := range allWorklogs {
		startLocal := wl.Start.In(time.Local)

		// Check if worklog belongs to current user
		var isCurrentUser bool
		if currentUser.ID != "" {
			isCurrentUser = wl.CreatedBy.ID == currentUser.ID
		} else {
			isCurrentUser = wl.CreatedBy.Display == currentUser.Display
		}

		// Check if worklog's start time falls within range
		if (startLocal.After(from) || startLocal.Equal(from)) &&
			(startLocal.Before(to.AddDate(0, 0, 1)) || startLocal.Equal(to)) &&
			isCurrentUser {
			worklogs = append(worklogs, wl)
		}
	}

	c.logger.Info("Worklogs retrieved and filtered for range",
		zap.Time("from", from),
		zap.Time("to", to),
		zap.Int("total_fetched", len(allWorklogs)),
		zap.Int("filtered_count", len(worklogs)))

	return worklogs, nil
}

// CreateWorklog creates a new worklog entry
func (c *Client) CreateWorklog(issueKey string, start time.Time, durationISO string, comment string) (*Worklog, error) {
	req := CreateWorklogRequest{
		Start:    start.Format("2006-01-02T15:04:05.000-0700"),
		Duration: durationISO,
		Comment:  comment,
	}

	var worklog Worklog
	path := fmt.Sprintf("/v2/issues/%s/worklog", issueKey)
	err := c.doRequest("POST", path, req, &worklog)
	if err != nil {
		return nil, fmt.Errorf("failed to create worklog for %s: %w", issueKey, err)
	}

	c.logger.Info("Worklog created",
		zap.String("issue", issueKey),
		zap.String("duration", durationISO),
		zap.Time("start", start))

	return &worklog, nil
}

// GetWorkedMinutesToday calculates total minutes worked today
func (c *Client) GetWorkedMinutesToday(date time.Time) (float64, error) {
	worklogs, err := c.GetWorklogsForToday(date)
	if err != nil {
		return 0, err
	}

	totalMinutes := 0.0
	for _, wl := range worklogs {
		minutes, err := ParseISO8601Duration(wl.Duration)
		if err != nil {
			c.logger.Warn("Failed to parse duration",
				zap.String("duration", wl.Duration),
				zap.Error(err))
			continue
		}
		totalMinutes += minutes
	}

	c.logger.Info("Total worked time calculated",
		zap.Time("date", date),
		zap.Float64("total_minutes", totalMinutes),
		zap.Float64("total_hours", totalMinutes/60))

	return totalMinutes, nil
}

// GetChangelog gets changelog (history of changes) for an issue
func (c *Client) GetChangelog(issueKey string) ([]ChangelogEntry, error) {
	var changelog []ChangelogEntry
	err := c.doRequest("GET", fmt.Sprintf("/v2/issues/%s/changelog", issueKey), nil, &changelog)
	if err != nil {
		return nil, fmt.Errorf("failed to get changelog for %s: %w", issueKey, err)
	}

	c.logger.Info("Changelog retrieved",
		zap.String("issue", issueKey),
		zap.Int("changes_count", len(changelog)))

	return changelog, nil
}

// DeleteWorklog deletes a worklog entry
func (c *Client) DeleteWorklog(issueKey string, worklogID string) error {
	err := c.doRequest("DELETE", fmt.Sprintf("/v2/issues/%s/worklog/%s", issueKey, worklogID), nil, nil)
	if err != nil {
		return fmt.Errorf("failed to delete worklog %s for %s: %w", worklogID, issueKey, err)
	}

	c.logger.Info("Worklog deleted",
		zap.String("issue", issueKey),
		zap.String("worklog_id", worklogID))

	return nil
}

// doRequest performs HTTP request with authentication
func (c *Client) doRequest(method, path string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonData)
	}

	url := c.baseURL + path

	var lastErr error
	for attempt := 1; attempt <= defaultRetries; attempt++ {
		err := c.doRequestOnce(method, url, bodyReader, result)
		if err == nil {
			return nil
		}

		lastErr = err
		c.logger.Warn("Request failed, retrying",
			zap.Int("attempt", attempt),
			zap.Int("max_retries", defaultRetries),
			zap.Error(err))

		if attempt < defaultRetries {
			time.Sleep(time.Second * time.Duration(attempt))
		}
	}

	return fmt.Errorf("request failed after %d attempts: %w", defaultRetries, lastErr)
}

// doRequestOnce performs a single HTTP request
func (c *Client) doRequestOnce(method, url string, body io.Reader, result interface{}) error {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Get IAM token
	token, err := c.tokenManager.GetToken()
	if err != nil {
		return fmt.Errorf("failed to get IAM token: %w", err)
	}

	// Set headers
	// IMPORTANT: For Cloud Organizations with IAM tokens (SSO/federated accounts):
	// - Use "Bearer" scheme (NOT "OAuth") for IAM tokens
	// - Use "X-Cloud-Org-Id" header (NOT "X-Org-ID") for Cloud Organizations
	// - For 360 Organizations, use "X-Org-ID" instead
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Cloud-Org-Id", c.orgID)
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
	}

	return nil
}

// ParseISO8601Duration parses ISO 8601 duration to minutes
// Yandex Tracker uses BUSINESS time units: 1 day = 8 hours, 1 week = 5 days (40 hours)
// Supported formats:
//   - PT8H -> 480 min (8 hours)
//   - P1D -> 480 min (1 day = 8 hours)
//   - P1W -> 2400 min (1 week = 40 hours)
//   - P1W2D -> 3360 min (1 week + 2 days = 56 hours)
//   - P1WT20M -> 2420 min (1 week + 20 minutes)
//   - P2DT3H30M -> 1170 min (2 days + 3.5 hours = 19.5 hours)
//   - PT1H30M -> 90 min (1.5 hours)
func ParseISO8601Duration(duration string) (float64, error) {
	if duration == "" {
		return 0, fmt.Errorf("empty duration")
	}

	// Parser for ISO 8601 duration format
	// Format: P[nW][nD]T[nH][nM][nS]
	// IMPORTANT: Business time units - 1 day = 8 hours, 1 week = 5 days (40 hours)

	minutes := 0.0

	// Remove 'P' prefix
	if duration[0] != 'P' {
		return 0, fmt.Errorf("invalid duration format: must start with P")
	}
	duration = duration[1:]

	// Split by 'T' to separate date and time parts
	datePart := duration
	timePart := ""
	if idx := bytes.IndexByte([]byte(duration), 'T'); idx >= 0 {
		datePart = duration[:idx]
		timePart = duration[idx+1:]
	}

	// Parse date part (weeks and days)
	// 1 business week = 5 days * 8 hours = 40 hours
	// 1 business day = 8 hours
	if datePart != "" {
		// Parse weeks (PnW or PnWnD)
		if idx := bytes.IndexByte([]byte(datePart), 'W'); idx >= 0 {
			var weeks int
			fmt.Sscanf(datePart[:idx], "%d", &weeks)
			minutes += float64(weeks * 5 * 8 * 60) // 5 business days * 8 hours * 60 minutes
			datePart = datePart[idx+1:] // Continue parsing after W
		}

		// Parse days (PnD)
		if idx := bytes.IndexByte([]byte(datePart), 'D'); idx >= 0 {
			var days int
			fmt.Sscanf(datePart[:idx], "%d", &days)
			minutes += float64(days * 8 * 60) // 8 business hours * 60 minutes
		}
	}

	// Parse time part
	if timePart != "" {
		var hours, mins, secs int

		// Try to parse hours
		if idx := bytes.IndexByte([]byte(timePart), 'H'); idx >= 0 {
			fmt.Sscanf(timePart[:idx], "%d", &hours)
			timePart = timePart[idx+1:]
		}

		// Try to parse minutes
		if idx := bytes.IndexByte([]byte(timePart), 'M'); idx >= 0 {
			fmt.Sscanf(timePart[:idx], "%d", &mins)
			timePart = timePart[idx+1:]
		}

		// Try to parse seconds
		if idx := bytes.IndexByte([]byte(timePart), 'S'); idx >= 0 {
			fmt.Sscanf(timePart[:idx], "%d", &secs)
		}

		minutes += float64(hours*60 + mins + secs/60) // FIXED: was = instead of +=
	}

	return minutes, nil
}

// FormatDuration formats minutes to ISO 8601 duration
// Examples: 480 -> PT8H, 90 -> PT1H30M, 45 -> PT45M
func FormatDuration(minutes float64) string {
	if minutes == 0 {
		return "PT0M"
	}

	hours := int(minutes / 60)
	mins := int(minutes) % 60

	if hours > 0 && mins > 0 {
		return fmt.Sprintf("PT%dH%dM", hours, mins)
	} else if hours > 0 {
		return fmt.Sprintf("PT%dH", hours)
	} else {
		return fmt.Sprintf("PT%dM", mins)
	}
}
