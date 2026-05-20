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
// session list from a mock opencode server.
func TestFetchSessionsSuccess(t *testing.T) {
	want := []SessionInfo{
		{ID: "ses_abc", Title: "My first task", Agent: "plan", Model: SessionModel{ID: "claude-opus-4"}},
		{ID: "ses_def", Title: "Fix the bug", Agent: "code"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	sessions, errMsg := p.fetchSessionsFromURL(srv.URL+"/session", "", "")

	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if len(sessions) != len(want) {
		t.Fatalf("got %d sessions, want %d", len(sessions), len(want))
	}
	for i, s := range sessions {
		if s.ID != want[i].ID {
			t.Errorf("session[%d].ID = %q, want %q", i, s.ID, want[i].ID)
		}
		if s.Title != want[i].Title {
			t.Errorf("session[%d].Title = %q, want %q", i, s.Title, want[i].Title)
		}
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
	sessions, errMsg := p.fetchSessionsFromURL(srv.URL+"/session", "", "")

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
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	sessions, errMsg := p.fetchSessionsFromURL(srv.URL+"/session", "", "")

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
		_ = json.NewEncoder(w).Encode([]SessionInfo{})
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	_, errMsg := p.fetchSessionsFromURL(srv.URL+"/session", wantUser, wantPass)
	if errMsg != "" {
		t.Fatalf("unexpected error with correct credentials: %s", errMsg)
	}

	_, errMsg = p.fetchSessionsFromURL(srv.URL+"/session", wantUser, "wrong")
	if errMsg == "" {
		t.Error("expected error with wrong credentials")
	}
}
