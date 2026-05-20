package console

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"
)

// renderDashboard is a test helper that renders the dashboard template with
// the given data and returns the output as a string.
func renderDashboard(t *testing.T, data any) string {
	t.Helper()
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"formatTime": func(ts time.Time) string {
			if ts.IsZero() {
				return "—"
			}
			return ts.Format("2006-01-02 15:04")
		},
	}).ParseFS(templateFS, "templates/index.html")
	if err != nil {
		t.Fatalf("parsing template: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "index.html", data); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return buf.String()
}

// TestDashboardEmptyState verifies the empty-state branch (no environments).
func TestDashboardEmptyState(t *testing.T) {
	data := struct {
		Envs        []EnvSnapshot
		LastUpdated time.Time
	}{}
	html := renderDashboard(t, data)
	if !strings.Contains(html, "No environments found") {
		t.Error("expected 'No environments found' in empty-state output")
	}
	if strings.Contains(html, `<div class="env-card"`) {
		t.Error("did not expect env-card div in empty-state output")
	}
}

// TestDashboardEnvWithSessions verifies that environment name, session title,
// model, and the deep-link URL all appear correctly.
func TestDashboardEnvWithSessions(t *testing.T) {
	created := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	updated := time.Date(2025, 5, 10, 14, 30, 0, 0, time.UTC)

	env := EnvSnapshot{
		Name:    "myenv",
		Status:  "running",
		Workdir: "/home/user/project",
		Created: created,
		Sessions: []SessionInfo{
			{
				ID:        "ses_abc123",
				Title:     "Implement feature X",
				Agent:     "plan",
				Model:     SessionModel{ID: "claude-opus-4", ProviderID: "anthropic"},
				Directory: "/data",
				UpdatedMS: updated.UnixMilli(),
				Status:    SessionStatus{Type: "busy"},
			},
		},
	}

	data := struct {
		Envs        []EnvSnapshot
		LastUpdated time.Time
	}{
		Envs:        []EnvSnapshot{env},
		LastUpdated: time.Now(),
	}

	html := renderDashboard(t, data)

	// Environment metadata.
	if !strings.Contains(html, "myenv") {
		t.Error("expected environment name 'myenv' in output")
	}
	if !strings.Contains(html, "running") {
		t.Error("expected status 'running' in output")
	}
	if !strings.Contains(html, "/home/user/project") {
		t.Error("expected workdir in output")
	}

	// Session data.
	if !strings.Contains(html, "Implement feature X") {
		t.Error("expected session title in output")
	}
	if !strings.Contains(html, "plan") {
		t.Error("expected agent 'plan' in output")
	}
	if !strings.Contains(html, "claude-opus-4") {
		t.Error("expected model ID in output")
	}

	// Deep link URL: /<urlsafe-base64("/data")>/session/<id>
	// base64.RawURLEncoding("/data") == "L2RhdGE"
	wantURL := "http://myenv.pinchy.localhost:4096/L2RhdGE/session/ses_abc123"
	if !strings.Contains(html, wantURL) {
		t.Errorf("expected deep-link URL %q in output\ngot:\n%s", wantURL, html)
	}

	// Env-level web console link.
	wantConsoleLink := "http://myenv.pinchy.localhost:4096/"
	if !strings.Contains(html, wantConsoleLink) {
		t.Errorf("expected env console link %q in output", wantConsoleLink)
	}

	// Session status badge should show "running" for a busy session.
	if !strings.Contains(html, "badge-busy") {
		t.Error("expected badge-busy class in output for busy session")
	}
	if !strings.Contains(html, "running") {
		t.Error("expected 'running' status label in output for busy session")
	}
}

// TestDashboardSessionStatusBadges verifies that all three session status
// variants (idle, busy, retry) render the correct badge.
func TestDashboardSessionStatusBadges(t *testing.T) {
	env := EnvSnapshot{
		Name:   "testenv",
		Status: "running",
		Sessions: []SessionInfo{
			{ID: "ses_1", Title: "Idle session", Status: SessionStatus{Type: "idle"}},
			{ID: "ses_2", Title: "Busy session", Status: SessionStatus{Type: "busy"}},
			{ID: "ses_3", Title: "Retry session", Status: SessionStatus{Type: "retry", Attempt: 1, Message: "rate limited"}},
			{ID: "ses_4", Title: "Question session", Status: SessionStatus{Type: "question"}},
			{ID: "ses_5", Title: "Permission session", Status: SessionStatus{Type: "permission"}},
		},
	}

	data := struct {
		Envs        []EnvSnapshot
		LastUpdated time.Time
	}{
		Envs:        []EnvSnapshot{env},
		LastUpdated: time.Now(),
	}

	html := renderDashboard(t, data)

	if !strings.Contains(html, "badge-idle") {
		t.Error("expected badge-idle in output")
	}
	if !strings.Contains(html, "waiting") {
		t.Error("expected 'waiting' label for idle session")
	}
	if !strings.Contains(html, "badge-busy") {
		t.Error("expected badge-busy in output")
	}
	if !strings.Contains(html, "badge-retry") {
		t.Error("expected badge-retry in output")
	}
	if !strings.Contains(html, "retrying") {
		t.Error("expected 'retrying' label for retry session")
	}
	// Retry tooltip should include the error message.
	if !strings.Contains(html, "rate limited") {
		t.Error("expected retry message 'rate limited' in output")
	}
	if !strings.Contains(html, "badge-question") {
		t.Error("expected badge-question in output")
	}
	if !strings.Contains(html, "needs input") {
		t.Error("expected 'needs input' label for question session")
	}
	if !strings.Contains(html, "badge-permission") {
		t.Error("expected badge-permission in output")
	}
	if !strings.Contains(html, "needs approval") {
		t.Error("expected 'needs approval' label for permission session")
	}
}

// TestDashboardFetchError verifies that a fetch error is rendered in the
// appropriate warning div rather than a sessions table.
func TestDashboardFetchError(t *testing.T) {
	env := EnvSnapshot{
		Name:       "errenv",
		Status:     "stopped",
		FetchError: "agent is Exited (1) 5s ago",
	}
	data := struct {
		Envs        []EnvSnapshot
		LastUpdated time.Time
	}{
		Envs:        []EnvSnapshot{env},
		LastUpdated: time.Now(),
	}
	html := renderDashboard(t, data)

	if !strings.Contains(html, "errenv") {
		t.Error("expected env name in output")
	}
	if !strings.Contains(html, "agent is Exited (1) 5s ago") {
		t.Error("expected fetch error message in output")
	}
	if strings.Contains(html, "<table>") {
		t.Error("did not expect a sessions table when FetchError is set")
	}
}

// TestDashboardNoSessions verifies that the "no sessions yet" notice is
// rendered when sessions is nil/empty and no error is set.
func TestDashboardNoSessions(t *testing.T) {
	env := EnvSnapshot{
		Name:   "emptyenv",
		Status: "running",
	}
	data := struct {
		Envs        []EnvSnapshot
		LastUpdated time.Time
	}{
		Envs:        []EnvSnapshot{env},
		LastUpdated: time.Now(),
	}
	html := renderDashboard(t, data)

	if !strings.Contains(html, "No opencode sessions yet") {
		t.Error("expected 'No opencode sessions yet' notice in output")
	}
}

// TestWebConsoleURL verifies the URL construction for session deep links.
func TestWebConsoleURL(t *testing.T) {
	e := EnvSnapshot{Name: "staging"}

	cases := []struct {
		dir     string
		id      string
		want    string
	}{
		{
			dir:  "/data",
			id:   "ses_xyz789",
			// base64.RawURLEncoding("/data") == "L2RhdGE"
			want: "http://staging.pinchy.localhost:4096/L2RhdGE/session/ses_xyz789",
		},
		{
			dir:  "/home/user/project",
			id:   "ses_abc",
			// base64.RawURLEncoding("/home/user/project") == "L2hvbWUvdXNlci9wcm9qZWN0"
			want: "http://staging.pinchy.localhost:4096/L2hvbWUvdXNlci9wcm9qZWN0/session/ses_abc",
		},
		{
			dir:  "",
			id:   "ses_xyz",
			// empty directory falls back to env root
			want: "http://staging.pinchy.localhost:4096/",
		},
	}

	for _, tc := range cases {
		got := e.WebConsoleURL(tc.dir, tc.id)
		if got != tc.want {
			t.Errorf("WebConsoleURL(%q, %q) = %q, want %q", tc.dir, tc.id, got, tc.want)
		}
	}
}

// TestSnapshotConcurrency does a basic sanity check that Update and Read can
// be called from concurrent goroutines without panicking.
func TestSnapshotConcurrency(t *testing.T) {
	s := &Snapshot{}
	done := make(chan struct{})

	// Writer goroutine.
	go func() {
		for i := 0; i < 100; i++ {
			s.Update([]EnvSnapshot{{Name: "env"}})
		}
		close(done)
	}()

	// Reader goroutine.
	for i := 0; i < 100; i++ {
		s.Read()
	}
	<-done
}
