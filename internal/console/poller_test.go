package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

// TestFirstWord checks the firstWord helper.
func TestFirstWord(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Up 3 minutes", "up"},
		{"Exited (0) 5s ago", "exited"},
		{"", ""},
		{"running", "running"},
		{"  spaces  here  ", "spaces"},
	}
	for _, tc := range cases {
		got := firstWord(tc.input)
		if got != tc.want {
			t.Errorf("firstWord(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestResolveEnvStatus exercises the status-word mapping logic.
func TestResolveEnvStatus(t *testing.T) {
	cases := []struct {
		name         string
		agentStatus  string
		dockerStatus string
		dockerHealth string
		want         string
	}{
		{
			name:         "both running",
			agentStatus:  "Up 5 minutes",
			dockerStatus: "Up 5 minutes",
			dockerHealth: "healthy",
			want:         "running",
		},
		{
			name:         "agent exited",
			agentStatus:  "Exited (1) 10s ago",
			dockerStatus: "Up 5 minutes",
			want:         "stopped",
		},
		{
			name:         "docker unhealthy",
			agentStatus:  "Up 5 minutes",
			dockerStatus: "Up 5 minutes",
			dockerHealth: "unhealthy",
			want:         "unhealthy",
		},
		{
			name:         "docker starting health",
			agentStatus:  "Up 5 minutes",
			dockerStatus: "Up 5 minutes",
			dockerHealth: "starting",
			want:         "starting",
		},
		{
			name:         "both empty",
			agentStatus:  "",
			dockerStatus: "",
			want:         "unknown",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := pinchyenv.Environment{
				AgentStatus:  tc.agentStatus,
				DockerStatus: tc.dockerStatus,
				DockerHealth: tc.dockerHealth,
			}
			got := resolveEnvStatus(e)
			if got != tc.want {
				t.Errorf("resolveEnvStatus = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFetchSessionsSuccess verifies that fetchSessions decodes a valid JSON
// session list from a mock opencode server and merges in session status.
func TestFetchSessionsSuccess(t *testing.T) {
	wantSessions := []SessionInfo{
		{ID: "ses_abc", Title: "My first task", Agent: "plan", Model: SessionModel{ID: "claude-opus-4"}},
		{ID: "ses_def", Title: "Fix the bug", Agent: "code"},
	}
	wantStatus := map[string]SessionStatus{
		"ses_abc": {Type: "busy"},
		"ses_def": {Type: "idle"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			_ = json.NewEncoder(w).Encode(wantSessions)
		case "/session/status":
			_ = json.NewEncoder(w).Encode(wantStatus)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	sessions, errMsg := p.fetchSessionsFromURL(srv.URL+"/session", srv.URL+"/session/status", "", "")

	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if len(sessions) != len(wantSessions) {
		t.Fatalf("got %d sessions, want %d", len(sessions), len(wantSessions))
	}
	for i, s := range sessions {
		if s.ID != wantSessions[i].ID {
			t.Errorf("session[%d].ID = %q, want %q", i, s.ID, wantSessions[i].ID)
		}
		if s.Title != wantSessions[i].Title {
			t.Errorf("session[%d].Title = %q, want %q", i, s.Title, wantSessions[i].Title)
		}
	}
	// Verify status was merged.
	if sessions[0].Status.Type != "busy" {
		t.Errorf("session[0].Status.Type = %q, want %q", sessions[0].Status.Type, "busy")
	}
	if sessions[1].Status.Type != "idle" {
		t.Errorf("session[1].Status.Type = %q, want %q", sessions[1].Status.Type, "idle")
	}
}

// TestFetchSessionsStatusFallback verifies that sessions are still returned
// (with idle status) when the /session/status endpoint fails.
func TestFetchSessionsStatusFallback(t *testing.T) {
	wantSessions := []SessionInfo{
		{ID: "ses_abc", Title: "My first task", Agent: "plan"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			_ = json.NewEncoder(w).Encode(wantSessions)
		case "/session/status":
			http.Error(w, "not implemented", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	sessions, errMsg := p.fetchSessionsFromURL(srv.URL+"/session", srv.URL+"/session/status", "", "")

	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	// Status should default to idle when /session/status fails.
	if sessions[0].Status.Type != "idle" {
		t.Errorf("session[0].Status.Type = %q, want %q", sessions[0].Status.Type, "idle")
	}
}

// TestFetchSessionsRetryStatus verifies that retry status fields are decoded
// correctly and merged into the session.
func TestFetchSessionsRetryStatus(t *testing.T) {
	wantSessions := []SessionInfo{
		{ID: "ses_abc", Title: "Retrying task", Agent: "plan"},
	}
	wantStatus := map[string]SessionStatus{
		"ses_abc": {Type: "retry", Attempt: 2, Message: "rate limited", Next: 1700000000000},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			_ = json.NewEncoder(w).Encode(wantSessions)
		case "/session/status":
			_ = json.NewEncoder(w).Encode(wantStatus)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	sessions, errMsg := p.fetchSessionsFromURL(srv.URL+"/session", srv.URL+"/session/status", "", "")

	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if sessions[0].Status.Type != "retry" {
		t.Errorf("Status.Type = %q, want %q", sessions[0].Status.Type, "retry")
	}
	if sessions[0].Status.Attempt != 2 {
		t.Errorf("Status.Attempt = %d, want 2", sessions[0].Status.Attempt)
	}
	if sessions[0].Status.Message != "rate limited" {
		t.Errorf("Status.Message = %q, want %q", sessions[0].Status.Message, "rate limited")
	}
}

// TestFetchSessionsUnauthorized verifies that a 401 response is converted into
// a descriptive error string (not a Go error that would panic).
func TestFetchSessionsUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="opencode"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	sessions, errMsg := p.fetchSessionsFromURL(srv.URL+"/session", srv.URL+"/session/status", "", "")

	if sessions != nil {
		t.Errorf("expected nil sessions on 401, got %v", sessions)
	}
	if errMsg == "" {
		t.Error("expected non-empty errMsg on 401")
	}
}

// TestFetchSessionsServerError verifies that a 5xx response produces an error
// message without panicking.
func TestFetchSessionsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/session" {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	sessions, errMsg := p.fetchSessionsFromURL(srv.URL+"/session", srv.URL+"/session/status", "", "")

	if sessions != nil {
		t.Errorf("expected nil sessions on 500, got %v", sessions)
	}
	if errMsg == "" {
		t.Error("expected non-empty errMsg on 500")
	}
}

// TestFetchSessionsBasicAuth verifies that Basic Auth credentials are forwarded
// when a password is supplied.
func TestFetchSessionsBasicAuth(t *testing.T) {
	const wantUser = "opencode"
	const wantPass = "secret"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != wantUser || p != wantPass {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			_ = json.NewEncoder(w).Encode([]SessionInfo{})
		case "/session/status":
			_ = json.NewEncoder(w).Encode(map[string]SessionStatus{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	_, errMsg := p.fetchSessionsFromURL(srv.URL+"/session", srv.URL+"/session/status", wantUser, wantPass)
	if errMsg != "" {
		t.Fatalf("unexpected error with correct credentials: %s", errMsg)
	}

	_, errMsg = p.fetchSessionsFromURL(srv.URL+"/session", srv.URL+"/session/status", wantUser, "wrong")
	if errMsg == "" {
		t.Error("expected error with wrong credentials")
	}
}
