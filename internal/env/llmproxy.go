package env

// LLM proxy constants shared between the Go runtime and the baked images.
// Any change here MUST be reflected in:
//   - images/llmproxy/config.yaml  (general_settings.master_key)
//   - images/agent/Dockerfile      (ENV ANTHROPIC_API_KEY)
//   - images/agent/opencode.json   (provider.anthropic.options.baseURL)
//
// A test in internal/env/llmproxy_test.go enforces that these constants
// stay in sync with the baked config file.
const (
	// LLMProxyToken is the shared bearer token that agent containers present
	// when calling the LLM proxy. It is intentionally not secret — it only
	// authorises calls to the local pinchy-llmproxy container on the
	// pinchy-shared bridge network, never directly to Anthropic. The real
	// ANTHROPIC_API_KEY lives only inside the llmproxy container's environment,
	// sourced from the host's pinchy config file at container start time.
	LLMProxyToken = "sk-pinchy-llmproxy-shared"

	// LLMProxyPort is the TCP port the LLM proxy listens on inside the
	// pinchy-shared network. No host port is bound — it is internal only.
	LLMProxyPort = "4000"

	// LLMProxyBaseURL is the full base URL that opencode's Anthropic provider
	// should use. Baked into images/agent/opencode.json.
	LLMProxyBaseURL = "http://pinchy-llmproxy:" + LLMProxyPort + "/anthropic/v1"
)
