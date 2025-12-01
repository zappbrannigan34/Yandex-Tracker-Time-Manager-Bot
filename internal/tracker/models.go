package tracker

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// FlexibleID handles both string and number IDs from Tracker API
// Yandex Tracker API returns ID fields inconsistently:
// - Sometimes as number: 123456
// - Sometimes as hex string: "664c9a087b21446730da802d"
// This type automatically converts both formats to string
type FlexibleID string

// UnmarshalJSON implements json.Unmarshaler for FlexibleID
func (f *FlexibleID) UnmarshalJSON(b []byte) error {
	// Try to unmarshal as string first
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = FlexibleID(s)
		return nil
	}

	// Try as number
	var n int64
	if err := json.Unmarshal(b, &n); err == nil {
		*f = FlexibleID(strconv.FormatInt(n, 10))
		return nil
	}

	return fmt.Errorf("FlexibleID: cannot unmarshal %s", string(b))
}

// MarshalJSON implements json.Marshaler for FlexibleID
func (f FlexibleID) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(f))
}

// String returns string representation
func (f FlexibleID) String() string {
	return string(f)
}

// TrackerTime is a custom time type that handles Yandex Tracker API time format
// Yandex Tracker API returns timestamps in format: 2024-05-22T17:06:54.875+0000
// Standard Go time.Time parsing doesn't handle +0000 without colon, so we need custom unmarshaler
type TrackerTime struct {
	time.Time
}

// UnmarshalJSON implements json.Unmarshaler for TrackerTime
func (t *TrackerTime) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	// Try multiple formats that Tracker API might return
	formats := []string{
		"2006-01-02T15:04:05.000-0700", // Main format: 2024-05-22T17:06:54.875+0000
		"2006-01-02T15:04:05.000Z0700", // Alternative without colon
		"2006-01-02T15:04:05-0700",     // Without milliseconds
		time.RFC3339,                   // Standard RFC3339
		time.RFC3339Nano,               // RFC3339 with nanoseconds
	}

	var parseErr error
	for _, format := range formats {
		parsed, err := time.Parse(format, s)
		if err == nil {
			t.Time = parsed
			return nil
		}
		parseErr = err
	}

	return parseErr
}

// MarshalJSON implements json.Marshaler for TrackerTime
func (t TrackerTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Time.Format("2006-01-02T15:04:05.000-0700"))
}

// Issue represents a Yandex Tracker issue
type Issue struct {
	Self       string       `json:"self"`
	ID         FlexibleID   `json:"id"` // Can be string or number from API
	Key        string       `json:"key"`
	Version    int          `json:"version"`
	Summary    string       `json:"summary"`
	Type       *IssueType   `json:"type,omitempty"`
	Status     Status       `json:"status"`
	Assignee   *User        `json:"assignee,omitempty"`
	CreatedAt  TrackerTime  `json:"createdAt"`
	UpdatedAt  TrackerTime  `json:"updatedAt"`
	ResolvedAt *TrackerTime `json:"resolvedAt,omitempty"`
}

// IssueType represents issue type (Task, Epic, Bug, etc.)
type IssueType struct {
	ID      FlexibleID `json:"id"`
	Key     string     `json:"key"`
	Display string     `json:"display"`
}

// Status represents issue status
type Status struct {
	Self    string     `json:"self"`
	ID      FlexibleID `json:"id"` // Can be string or number from API
	Key     string     `json:"key"`
	Display string     `json:"display"`
}

// User represents a user
type User struct {
	Self    string     `json:"self"`
	ID      FlexibleID `json:"id"` // Can be string or number from API
	Display string     `json:"display"`
}

// Worklog represents a time tracking entry
type Worklog struct {
	Self      string      `json:"self"`
	ID        FlexibleID  `json:"id"` // Can be string or number from API
	Issue     IssueRef    `json:"issue"`
	Start     TrackerTime `json:"start"`
	Duration  string      `json:"duration"` // ISO 8601 format: PT1H30M
	Comment   string      `json:"comment,omitempty"`
	CreatedBy User        `json:"createdBy"`
	CreatedAt TrackerTime `json:"createdAt"`
}

// IssueRef represents a reference to an issue
type IssueRef struct {
	Self    string     `json:"self"`
	ID      FlexibleID `json:"id"` // Can be string or number from API
	Key     string     `json:"key"`
	Display string     `json:"display"`
}

// SearchIssuesRequest represents request to search issues
type SearchIssuesRequest struct {
	Query   string                 `json:"query,omitempty"`
	Filter  map[string]interface{} `json:"filter,omitempty"`
	Order   string                 `json:"order,omitempty"`
	Expand  string                 `json:"expand,omitempty"`
	PerPage int                    `json:"perPage,omitempty"`
}

// SearchWorklogsRequest represents request to search worklogs
type SearchWorklogsRequest struct {
	CreatedBy string     `json:"createdBy,omitempty"`
	CreatedAt *TimeRange `json:"createdAt,omitempty"`
}

// TimeRange represents a time range
type TimeRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// CreateWorklogRequest represents request to create worklog
type CreateWorklogRequest struct {
	Start    string `json:"start"`    // ISO 8601: 2025-01-15T10:00:00.000+0000
	Duration string `json:"duration"` // ISO 8601: PT1H30M
	Comment  string `json:"comment,omitempty"`
}

// WorklogSummary represents summary of worked time
type WorklogSummary struct {
	TotalMinutes float64
	Entries      []Worklog
}

// TimeEntry represents a time entry to be logged
type TimeEntry struct {
	IssueKey string
	Minutes  float64
	Comment  string
}

// ChangelogEntry represents a single change in issue history
type ChangelogEntry struct {
	ID        FlexibleID    `json:"id"`
	Self      string        `json:"self"`
	Issue     IssueRef      `json:"issue"`
	UpdatedAt TrackerTime   `json:"updatedAt"`
	UpdatedBy User          `json:"updatedBy"`
	Type      string        `json:"type"` // "IssueCreated", "IssueUpdated", etc.
	Transport string        `json:"transport,omitempty"`
	Fields    []FieldChange `json:"fields"`
}

// FieldChange represents a change in a single field
type FieldChange struct {
	Field FieldInfo   `json:"field"`
	From  interface{} `json:"from,omitempty"`
	To    interface{} `json:"to,omitempty"`
}

// FieldInfo represents metadata about a field
type FieldInfo struct {
	Self    string `json:"self"`
	ID      string `json:"id"` // "status", "assignee", etc.
	Display string `json:"display"`
}
