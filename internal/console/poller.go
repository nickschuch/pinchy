package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/client"

	"github.com/nickschuch/pinchy/internal/dockerx"
	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

// pollInterval is the time between full polling cycles. Each cycle re-discovers
// all pinchy environments and attempts to fetch sessions from running agents.
const pollInterval = 15 * time.Second

// httpTimeout is the per-request deadline when calling an agent's opencode
// server. Kept short so a slow agent doesn't stall the whole cycle.
const httpTimeout = 10 * time.Second

// Poller discovers pinchy environments from Docker and fetches opencode
// sessions from each running agent. It writes results into snap on every cycle.
type Poller struct {
	cli  *client.Client
	snap *Snapshot
	http *http.Client
}

// NewPoller creates a Poller that writes into snap.
func NewPoller(cli *client.Client, snap *Snapshot) *Poller {
	return &Poller{
		cli:  cli,
		snap: snap,
		http: &http.Client{Timeout: httpTimeout},
	}
}

// Run starts the polling loop. It performs one cycle immediately, then repeats
// every pollInterval until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	p.poll(ctx)
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.poll(ctx)
		}
	}
}

// poll performs a single discovery + session-fetch cycle and updates the
// snapshot. Errors for individual environments are captured in FetchError and
// do not abort the whole cycle.
func (p *Poller) poll(ctx context.Context) {
	envs, err := dockerx.ListEnvs(ctx, p.cli)
	if err != nil {
		// Docker unreachable — leave the last known snapshot in place.
		return
	}

	snaps := make([]EnvSnapshot, 0, len(envs))
	for _, e := range envs {
		snap := envSnapshotFrom(e)
		if e.AgentRunning {
			snap.Sessions, snap.FetchError = p.fetchSessions(ctx, e)
		} else {
			snap.FetchError = fmt.Sprintf("agent is %s", e.AgentStatus)
		}
		snap.LastFetched = time.Now()
		snaps = append(snaps, snap)
	}

	p.snap.Update(snaps)
}

// envSnapshotFrom converts a dockerx Environment into an EnvSnapshot, computing
// the status string the same way ls.go does (to stay consistent).
func envSnapshotFrom(e pinchyenv.Environment) EnvSnapshot {
	status := resolveEnvStatus(e)
	return EnvSnapshot{
		Name:           e.Name,
		Status:         status,
		Workdir:        e.Workdir,
		WorktreeRepo:   e.WorktreeRepo,
		WorktreeBranch: e.WorktreeBranch,
		Created:        e.Created,
	}
}

// firstWord returns the first whitespace-delimited word of s in lowercase.
// Returns "" for an empty string.
func firstWord(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(fields[0])
}

// resolveEnvStatus converts an environment's container state into a single
// human-readable status word. It mirrors the logic in internal/cli/ls.go.
func resolveEnvStatus(e pinchyenv.Environment) string {
	agentFirst := firstWord(e.AgentStatus)
	dockerFirst := firstWord(e.DockerStatus)

	agentBad := agentFirst == "exited" || agentFirst == "stopped" || agentFirst == "dead" || agentFirst == "removing"
	dockerBad := dockerFirst == "exited" || dockerFirst == "stopped" || dockerFirst == "dead" || dockerFirst == "removing"
	agentPaused := agentFirst == "paused"
	dockerPaused := dockerFirst == "paused"
	agentGood := agentFirst == "up" || agentFirst == "running"
	dockerGood := dockerFirst == "up" || dockerFirst == "running"
	agentStarting := agentFirst == "created" || agentFirst == "restarting" || agentFirst == "starting"
	dockerStarting := dockerFirst == "created" || dockerFirst == "restarting" || dockerFirst == "starting"

	switch {
	case agentBad || dockerBad:
		return "stopped"
	case agentPaused || dockerPaused:
		return "paused"
	case e.DockerHealth == "unhealthy":
		return "unhealthy"
	case agentStarting || dockerStarting || e.DockerHealth == "starting":
		return "starting"
	case agentGood && dockerGood:
		return "running"
	default:
		return "unknown"
	}
}

// fetchSessions calls GET /session on the agent's opencode server, using
// Basic Auth if OPENCODE_SERVER_PASSWORD is set in the container's env.
// It also fetches GET /session/status to enrich each session with its live
// running state (idle / busy / retry).
func (p *Poller) fetchSessions(ctx context.Context, e pinchyenv.Environment) ([]SessionInfo, string) {
	username, password := p.resolveAuth(ctx, e)
	// The console container lives on pinchy-shared, same as the agents.
	// Container-to-container DNS resolves pinchy-<env>-agent by name.
	base := "http://" + pinchyenv.AgentContainerName(e.Name) + ":4096"
	return p.fetchSessionsFromURL(base+"/session", base+"/session/status", username, password)
}

// fetchSessionsFromURL is the low-level implementation of fetchSessions. It is
// extracted so tests can supply an arbitrary URL (via httptest.Server) without
// needing a real Docker daemon.
func (p *Poller) fetchSessionsFromURL(sessionsURL, statusURL, username, password string) ([]SessionInfo, string) {
	sessions, fetchErr := p.fetchSessionList(sessionsURL, username, password)
	if fetchErr != "" {
		return nil, fetchErr
	}

	// Fetch live session statuses and merge them into the session list.
	// A failure here is non-fatal — sessions are still shown, just without
	// the live status indicator.
	statusMap, _ := p.fetchSessionStatusMap(statusURL, username, password)
	for i := range sessions {
		if st, ok := statusMap[sessions[i].ID]; ok {
			sessions[i].Status = st
		} else {
			// Default to idle when status is not available.
			sessions[i].Status = SessionStatus{Type: "idle"}
		}
	}

	return sessions, ""
}

// fetchSessionList performs the GET /session request and returns the decoded
// session slice, or an error string on failure.
func (p *Poller) fetchSessionList(url, username, password string) ([]SessionInfo, string) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Sprintf("building request: %v", err)
	}
	if password != "" {
		req.SetBasicAuth(username, password)
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Sprintf("fetching sessions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, "authentication required (set OPENCODE_SERVER_PASSWORD)"
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Sprintf("opencode server returned %s", resp.Status)
	}

	var sessions []SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, fmt.Sprintf("decoding sessions: %v", err)
	}
	return sessions, ""
}

// fetchSessionStatusMap performs the GET /session/status request and returns
// a map of sessionID → SessionStatus. On any error it returns an empty map
// (callers treat status as optional).
func (p *Poller) fetchSessionStatusMap(url, username, password string) (map[string]SessionStatus, string) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Sprintf("building status request: %v", err)
	}
	if password != "" {
		req.SetBasicAuth(username, password)
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Sprintf("fetching session status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Sprintf("session/status returned %s", resp.Status)
	}

	var statusMap map[string]SessionStatus
	if err := json.NewDecoder(resp.Body).Decode(&statusMap); err != nil {
		return nil, fmt.Sprintf("decoding session status: %v", err)
	}
	return statusMap, ""
}

// resolveAuth inspects the agent container to find OPENCODE_SERVER_PASSWORD
// and OPENCODE_SERVER_USERNAME. Returns ("opencode", "") when no password is
// set, matching opencode's own default.
func (p *Poller) resolveAuth(ctx context.Context, e pinchyenv.Environment) (username, password string) {
	username = "opencode"
	vars, err := dockerx.AgentEnvVars(ctx, p.cli, pinchyenv.AgentContainerName(e.Name))
	if err != nil {
		return username, ""
	}
	if u := vars["OPENCODE_SERVER_USERNAME"]; u != "" {
		username = u
	}
	password = vars["OPENCODE_SERVER_PASSWORD"]
	return username, password
}
