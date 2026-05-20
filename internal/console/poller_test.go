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
// ses_abc is busy (no message fetch); ses_def is idle with a user last-message
// (stays idle after enrichment).
func TestFetchSessionsSuccess(t *testing.T) {
	wantSessions := []SessionInfo{
		{ID: "ses_abc", Title: "My first task", Agent: "plan", Model: SessionModel{ID: "claude-opus-4"}},
		{ID: "ses_def", Title: "Fix the bug", Agent: "code"},
	}
	wantStatus := map[string]SessionStatus{
		"ses_abc": {Type: "busy"},
		"ses_def": {Type: "idle"},
	}
	// ses_def: last message is from the user → stays idle after enrichment.
	defMessages := []lastMessageEntry{{}}
	defMessages[0].Info.Role = "user"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			_ = json.NewEncoder(w).Encode(wantSessions)
		case "/session/status":
			_ = json.NewEncoder(w).Encode(wantStatus)
		case "/session/ses_def/message":
			_ = json.NewEncoder(w).Encode(defMessages)
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
	// Last message is from the user → stays idle after enrichment.
	abcMessages := []lastMessageEntry{{}}
	abcMessages[0].Info.Role = "user"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			_ = json.NewEncoder(w).Encode(wantSessions)
		case "/session/status":
			http.Error(w, "not implemented", http.StatusInternalServerError)
		case "/session/ses_abc/message":
			_ = json.NewEncoder(w).Encode(abcMessages)
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

// TestFetchLastMessageQuestion verifies that an idle session whose last message
// is from the assistant (with no pending tool) is enriched to "question".
func TestFetchLastMessageQuestion(t *testing.T) {
	// Assistant message, no tool parts.
	msgs := []lastMessageEntry{{}}
	msgs[0].Info.Role = "assistant"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(msgs)
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	role, pending, err := p.fetchLastMessage(srv.URL+"/message", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != "assistant" {
		t.Errorf("role = %q, want %q", role, "assistant")
	}
	if pending {
		t.Error("expected hasPendingTool = false for assistant message with no tool parts")
	}

	st := p.enrichIdleStatus(srv.URL+"/message", "", "")
	if st.Type != "question" {
		t.Errorf("enrichIdleStatus = %q, want %q", st.Type, "question")
	}
}

// TestFetchLastMessagePermission verifies that an idle session whose last
// assistant message contains a pending tool part is enriched to "permission".
func TestFetchLastMessagePermission(t *testing.T) {
	msgs := []lastMessageEntry{{}}
	msgs[0].Info.Role = "assistant"
	msgs[0].Parts = []struct {
		Type  string `json:"type"`
		State struct {
			Status string `json:"status"`
		} `json:"state"`
	}{
		{Type: "tool", State: struct {
			Status string `json:"status"`
		}{Status: "pending"}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(msgs)
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	role, pending, err := p.fetchLastMessage(srv.URL+"/message", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != "assistant" {
		t.Errorf("role = %q, want %q", role, "assistant")
	}
	if !pending {
		t.Error("expected hasPendingTool = true for assistant message with pending tool part")
	}

	st := p.enrichIdleStatus(srv.URL+"/message", "", "")
	if st.Type != "permission" {
		t.Errorf("enrichIdleStatus = %q, want %q", st.Type, "permission")
	}
}

// TestFetchLastMessageUserRole verifies that an idle session whose last message
// is from the user stays idle after enrichment.
func TestFetchLastMessageUserRole(t *testing.T) {
	msgs := []lastMessageEntry{{}}
	msgs[0].Info.Role = "user"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(msgs)
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	st := p.enrichIdleStatus(srv.URL+"/message", "", "")
	if st.Type != "idle" {
		t.Errorf("enrichIdleStatus = %q, want %q", st.Type, "idle")
	}
}

// TestFetchLastMessageEmpty verifies that an empty message list leaves the
// session as idle.
func TestFetchLastMessageEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]lastMessageEntry{})
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	st := p.enrichIdleStatus(srv.URL+"/message", "", "")
	if st.Type != "idle" {
		t.Errorf("enrichIdleStatus = %q, want %q", st.Type, "idle")
	}
}

// TestFetchLastMessageFetchError verifies that a failed message fetch leaves
// the session as idle rather than propagating the error.
func TestFetchLastMessageFetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	st := p.enrichIdleStatus(srv.URL+"/message", "", "")
	if st.Type != "idle" {
		t.Errorf("enrichIdleStatus = %q, want %q", st.Type, "idle")
	}
}

// TestFetchSessionsQuestionEnrichment verifies the full pipeline: an idle
// session is enriched to "question" when its last message is from the assistant.
func TestFetchSessionsQuestionEnrichment(t *testing.T) {
	sessions := []SessionInfo{{ID: "ses_q", Title: "Needs reply", Agent: "plan"}}
	status := map[string]SessionStatus{"ses_q": {Type: "idle"}}
	msgs := []lastMessageEntry{{}}
	msgs[0].Info.Role = "assistant"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			_ = json.NewEncoder(w).Encode(sessions)
		case "/session/status":
			_ = json.NewEncoder(w).Encode(status)
		case "/session/ses_q/message":
			_ = json.NewEncoder(w).Encode(msgs)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	got, errMsg := p.fetchSessionsFromURL(srv.URL+"/session", srv.URL+"/session/status", "", "")
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got[0].Status.Type != "question" {
		t.Errorf("Status.Type = %q, want %q", got[0].Status.Type, "question")
	}
}

// TestFetchSessionsPermissionEnrichment verifies the full pipeline: an idle
// session is enriched to "permission" when its last assistant message contains
// a pending tool part.
func TestFetchSessionsPermissionEnrichment(t *testing.T) {
	sessions := []SessionInfo{{ID: "ses_p", Title: "Needs approval", Agent: "plan"}}
	status := map[string]SessionStatus{"ses_p": {Type: "idle"}}
	msgs := []lastMessageEntry{{}}
	msgs[0].Info.Role = "assistant"
	msgs[0].Parts = []struct {
		Type  string `json:"type"`
		State struct {
			Status string `json:"status"`
		} `json:"state"`
	}{
		{Type: "tool", State: struct {
			Status string `json:"status"`
		}{Status: "pending"}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			_ = json.NewEncoder(w).Encode(sessions)
		case "/session/status":
			_ = json.NewEncoder(w).Encode(status)
		case "/session/ses_p/message":
			_ = json.NewEncoder(w).Encode(msgs)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := &Poller{http: &http.Client{}}
	got, errMsg := p.fetchSessionsFromURL(srv.URL+"/session", srv.URL+"/session/status", "", "")
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got[0].Status.Type != "permission" {
		t.Errorf("Status.Type = %q, want %q", got[0].Status.Type, "permission")
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
