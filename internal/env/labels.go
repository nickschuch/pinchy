// Package env defines the data model for a pinchy environment.
//
// An environment is a logical group of Docker resources (containers, volumes,
// networks) that together provide a containerised development workspace
// consisting of an opencode-driven agent container and a dedicated rootless
// docker-in-docker daemon container. Resources are discovered exclusively via
// labels — pinchy holds no on-disk state.
package env

// Label keys applied to every Docker resource pinchy creates.
const (
	// LabelManaged marks a resource as owned by pinchy. Always set to "true".
	LabelManaged = "pinchy.managed"
	// LabelEnv is the environment name (DNS-safe identifier).
	LabelEnv = "pinchy.env"
	// LabelRole identifies the resource's function within the environment.
	LabelRole = "pinchy.role"
	// LabelWorkdir is the absolute host path bind-mounted into the agent
	// container at /data. Set on the agent container only.
	LabelWorkdir = "pinchy.workdir"
	// LabelWorktreeRepo is the absolute host path of the source git repository
	// from which a worktree was created. Set on the agent container only when
	// the environment was created via automatic git worktree support. Empty
	// when the environment uses a plain bind-mount.
	LabelWorktreeRepo = "pinchy.worktree.repo"
	// LabelWorktreeBranch is the name of the git branch that was created for
	// the worktree. Set on the agent container only when LabelWorktreeRepo is
	// also set.
	LabelWorktreeBranch = "pinchy.worktree.branch"
	// LabelCreated is an RFC3339 timestamp recording when pinchy created the
	// resource.
	LabelCreated = "pinchy.created"
	// LabelVersion is the pinchy version that created the resource.
	LabelVersion = "pinchy.version"
)

// Role values assigned to LabelRole.
const (
	RoleAgent         = "agent"
	RoleDocker        = "docker"
	RoleProxy         = "proxy"
	RoleConsole       = "console"
	RoleLLMProxy      = "llmproxy"
	RoleSharedNetwork = "shared-network"
)

// LabelLLMProxyKeyHash holds a short sha256 prefix of the real Anthropic API
// key. It is used solely for change detection: when the user rotates the key
// and re-runs `pinchy init`, EnsureLLMProxy sees a hash mismatch, removes the
// old container, and starts a fresh one with the updated env var. The full key
// is never written to any label.
const LabelLLMProxyKeyHash = "pinchy.llmproxy.keyhash"

// ManagedTrue is the canonical value for LabelManaged.
const ManagedTrue = "true"

// proxyPort pairs a Traefik entrypoint name with its port number and the path
// that Traefik's active healthcheck will probe on each backend. The path must
// return a 2xx response on healthy backends; Traefik stops routing to backends
// that fail the check, enabling automatic failover between the agent and dind.
type proxyPort struct {
	ep         string
	port       string
	healthPath string
}

// proxyPorts is the canonical list of entrypoints the shared proxy exposes.
// Health paths are chosen to match what actually runs on each port:
//   - :8080 user services (e.g. docker compose) expose /healthz by convention.
//   - :4096 opencode web server exposes /global/health.
//   - :3000 user services expose /healthz by convention.
var proxyPorts = []proxyPort{
	{ep: "p8080", port: "8080", healthPath: "/healthz"},
	{ep: "p4096", port: "4096", healthPath: "/global/health"},
	{ep: "p3000", port: "3000", healthPath: "/healthz"},
}

// BaseLabels returns the labels common to every resource in an environment.
func BaseLabels(envName, role, version string, createdRFC3339 string) map[string]string {
	return map[string]string{
		LabelManaged: ManagedTrue,
		LabelEnv:     envName,
		LabelRole:    role,
		LabelCreated: createdRFC3339,
		LabelVersion: version,
	}
}

// ServiceLabels returns labels for a global (non-environment-scoped) resource
// such as the shared network or proxy container. LabelEnv is intentionally
// omitted so that discovery by env name does not match these resources.
func ServiceLabels(role, version, createdRFC3339 string) map[string]string {
	return map[string]string{
		LabelManaged: ManagedTrue,
		LabelRole:    role,
		LabelCreated: createdRFC3339,
		LabelVersion: version,
	}
}

// proxyServiceLabels returns the loadbalancer service labels for a single
// entrypoint. These MUST be byte-identical on every container that contributes
// to the same Traefik service name; any difference causes Traefik to log:
//
//	"Service defined multiple times with different configurations"
//
// and drop the service entirely, resulting in 404 responses.
//
// healthPath is the HTTP path Traefik probes on each backend to determine
// liveness. It must return 2xx for the backend to receive traffic.
func proxyServiceLabels(svc, port, healthPath string) map[string]string {
	p := "traefik.http.services." + svc + ".loadbalancer."
	return map[string]string{
		p + "server.port":          port,
		p + "healthcheck.path":     healthPath,
		p + "healthcheck.interval": "5s",
		p + "healthcheck.timeout":  "2s",
	}
}

// ProxyTraefikLabelsAgent returns the Traefik Docker-provider labels to attach
// to the agent container. The agent declares the HTTP routers, services, and
// active healthcheck for each of the three proxy ports (8080, 4096, 3000).
//
// Traffic is routed by Host header: <envName>.pinchy.localhost on each port.
// Active healthchecks probe /healthz on every backend so that Traefik
// automatically stops forwarding to an unhealthy server (e.g. when the agent
// is not running an application on that port) and concentrates traffic on the
// healthy backend — typically the dind container when docker compose is in use.
func ProxyTraefikLabelsAgent(envName string) map[string]string {
	labels := map[string]string{
		"traefik.enable":         "true",
		"traefik.docker.network": SharedNetworkName,
	}
	for _, p := range proxyPorts {
		rtr := envName + "-" + p.ep
		svc := envName + "-" + p.ep
		labels["traefik.http.routers."+rtr+".rule"] = "Host(`" + envName + ".pinchy.localhost`)"
		labels["traefik.http.routers."+rtr+".entrypoints"] = p.ep
		labels["traefik.http.routers."+rtr+".service"] = svc
		for k, v := range proxyServiceLabels(svc, p.port, p.healthPath) {
			labels[k] = v
		}
	}
	return labels
}

// ConsoleTraefikLabels returns the Traefik Docker-provider labels for the
// console container. The console is a single global service (like the proxy)
// reachable at http://console.pinchy.localhost:8080 via the p8080 entrypoint.
// Its healthcheck probes /healthz on port 8080.
func ConsoleTraefikLabels() map[string]string {
	const (
		rtr        = "console-p8080"
		svc        = "console-p8080"
		serverPort = "8080"
		healthPath = "/healthz"
	)
	p := "traefik.http."
	return map[string]string{
		"traefik.enable":                                                        "true",
		"traefik.docker.network":                                                SharedNetworkName,
		p + "routers." + rtr + ".rule":                                          "Host(`console.pinchy.localhost`)",
		p + "routers." + rtr + ".entrypoints":                                   "p8080",
		p + "routers." + rtr + ".service":                                       svc,
		p + "services." + svc + ".loadbalancer.server.port":                     serverPort,
		p + "services." + svc + ".loadbalancer.healthcheck.path":                healthPath,
		p + "services." + svc + ".loadbalancer.healthcheck.interval":            "5s",
		p + "services." + svc + ".loadbalancer.healthcheck.timeout":             "2s",
	}
}

// LLMProxyTraefikLabels returns the Traefik Docker-provider labels for the
// LLM proxy container. The proxy admin UI is exposed as a single global service
// reachable at http://llmproxy.pinchy.localhost:8080 via the p8080 entrypoint.
// The healthcheck probes /health/liveliness on port 4000 (LiteLLM's standard
// liveness endpoint).
func LLMProxyTraefikLabels() map[string]string {
	const (
		rtr        = "llmproxy-p8080"
		svc        = "llmproxy-p8080"
		serverPort = "4000"
		healthPath = "/health/liveliness"
	)
	p := "traefik.http."
	return map[string]string{
		"traefik.enable":                                             "true",
		"traefik.docker.network":                                     SharedNetworkName,
		p + "routers." + rtr + ".rule":                              "Host(`llmproxy.pinchy.localhost`)",
		p + "routers." + rtr + ".entrypoints":                       "p8080",
		p + "routers." + rtr + ".service":                           svc,
		p + "services." + svc + ".loadbalancer.server.port":         serverPort,
		p + "services." + svc + ".loadbalancer.healthcheck.path":    healthPath,
		p + "services." + svc + ".loadbalancer.healthcheck.interval": "5s",
		p + "services." + svc + ".loadbalancer.healthcheck.timeout":  "2s",
	}
}

// ProxyTraefikLabelsDocker returns the Traefik Docker-provider labels to
// attach to the dind-rootless container. The dind container contributes the
// same service-level labels as the agent (byte-identical, which is required by
// Traefik) so that each service ends up with two backend servers. Active
// healthchecks on /healthz then route traffic exclusively to whichever server
// is healthy, providing automatic failover between the agent and dind.
//
// Port-published services running inside the inner docker daemon (e.g. via
// docker compose) are reachable at <dind-ip>:<port> on pinchy-shared because
// rootlesskit's port driver forwards published ports onto the dind container's
// network interfaces.
func ProxyTraefikLabelsDocker(envName string) map[string]string {
	labels := map[string]string{
		"traefik.enable":         "true",
		"traefik.docker.network": SharedNetworkName,
	}
	for _, p := range proxyPorts {
		svc := envName + "-" + p.ep
		for k, v := range proxyServiceLabels(svc, p.port, p.healthPath) {
			labels[k] = v
		}
	}
	return labels
}
