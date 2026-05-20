// Package console implements the pinchy discovery dashboard — a lightweight
// HTTP server that aggregates all pinchy environments and their opencode
// sessions into a single web page served at
// http://console.pinchy.localhost:8080.
package console

import (
	"encoding/base64"
	"sync"
	"time"
)

// SessionInfo holds the subset of opencode session data displayed on the
// dashboard. It is populated by decoding the JSON response from
// GET /session on each agent's opencode web server.
type SessionInfo struct {
	// ID is the opaque opencode session identifier (e.g. "ses_…").
	ID string `json:"id"`
	// Title is the human-readable session title shown in the opencode UI.
	Title string `json:"title"`
	// Agent is the agent that owns the session (e.g. "plan", "code").
	Agent string `json:"agent"`
	// Model is the model configuration for the session.
	Model SessionModel `json:"model"`
	// Directory is the absolute path that the opencode server was running in
	// when this session was created (e.g. "/data"). It is the first path
	// segment of the SPA deep-link URL, URL-safe-base64-encoded.
	Directory string `json:"directory"`
	// UpdatedMS is the "updated" timestamp in milliseconds since epoch.
	UpdatedMS int64 `json:"updated,omitempty"`
	// Status is the live status of the session fetched from GET /session/status.
	// It is not part of the opencode JSON response; it is populated separately.
	Status SessionStatus `json:"-"`
}

// SessionModel carries the provider and model ID embedded in a session.
type SessionModel struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
}

// SessionStatus represents the live running state of an opencode session.
//
// The Type field is one of:
//   - "idle"       — the session is quiet; the last message was from the user
//                    (or there are no messages yet)
//   - "busy"       — the agent is actively processing / running tools
//   - "retry"      — the agent hit an error and is retrying
//   - "question"   — synthetic: the session is idle but the agent sent the last
//                    message, meaning it is waiting for the user to reply
//   - "permission" — synthetic: the session is idle, the agent sent the last
//                    message, and that message contains a tool call that is
//                    blocked pending user approval
//
// "idle", "busy", and "retry" come directly from GET /session/status.
// "question" and "permission" are derived by the poller from the last message
// in GET /session/:id/message?limit=1 when the raw status is "idle".
//
// For "retry", Attempt, Message, and Next are populated.
type SessionStatus struct {
	// Type is the discriminant: "idle", "busy", "retry", "question", or "permission".
	Type string `json:"type"`
	// Attempt is the retry attempt number (only set when Type == "retry").
	Attempt int `json:"attempt,omitempty"`
	// Message is the retry error message (only set when Type == "retry").
	Message string `json:"message,omitempty"`
	// Next is the Unix millisecond timestamp of the next retry attempt
	// (only set when Type == "retry").
	Next int64 `json:"next,omitempty"`
}

// sessionInfoTime returns UpdatedMS as a time.Time. Zero when unset.
func (s SessionInfo) UpdatedTime() time.Time {
	if s.UpdatedMS == 0 {
		return time.Time{}
	}
	return time.UnixMilli(s.UpdatedMS)
}

// EnvSnapshot holds the dashboard data for a single pinchy environment
// captured during one polling cycle.
type EnvSnapshot struct {
	// Name is the pinchy environment name (DNS-safe).
	Name string
	// Status is the resolved human-readable status ("running", "stopped", …).
	Status string
	// Workdir is the host path bind-mounted into the agent.
	Workdir string
	// WorktreeRepo is the absolute host path of the source git repository from
	// which a worktree was created, or empty for plain bind-mounts.
	WorktreeRepo string
	// WorktreeBranch is the git branch name for the worktree, or empty.
	WorktreeBranch string
	// Created is the time the environment was created.
	Created time.Time
	// Sessions holds the opencode sessions discovered inside the environment.
	// Nil when the agent is not running or the fetch failed.
	Sessions []SessionInfo
	// FetchError is set when the /session fetch failed (e.g. agent not
	// running, authentication error). Empty when successful.
	FetchError string
	// LastFetched is the time this snapshot entry was last attempted.
	LastFetched time.Time
}

// WebConsoleURL returns the deep-link URL that opens a specific session
// directly in the opencode web UI for this environment.
//
// The opencode SPA routes sessions at /<dir-slug>/session/<id>, where
// <dir-slug> is the URL-safe base64 encoding (no padding) of the session's
// directory path — matching the `mt()` function in the opencode frontend
// bundle. When directory is empty the env-root URL is returned instead.
func (e EnvSnapshot) WebConsoleURL(directory, sessionID string) string {
	base := "http://" + e.Name + ".pinchy.localhost:4096"
	if directory == "" {
		return base + "/"
	}
	slug := base64.RawURLEncoding.EncodeToString([]byte(directory))
	return base + "/" + slug + "/session/" + sessionID
}

// Snapshot is the full dashboard data set, protected by a RWMutex so the HTTP
// handler and poller goroutine can access it concurrently without blocking one
// another for long.
type Snapshot struct {
	mu          sync.RWMutex
	envs        []EnvSnapshot
	lastUpdated time.Time
}

// Update atomically replaces the snapshot contents.
func (s *Snapshot) Update(envs []EnvSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.envs = envs
	s.lastUpdated = time.Now()
}

// Read returns a consistent copy of the snapshot data. The returned slice is a
// shallow copy; callers must not modify the individual EnvSnapshot values.
func (s *Snapshot) Read() ([]EnvSnapshot, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]EnvSnapshot, len(s.envs))
	copy(cp, s.envs)
	return cp, s.lastUpdated
}
