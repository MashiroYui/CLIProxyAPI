// Package config provides configuration management for the CLI Proxy API server.
// It handles loading and parsing YAML configuration files, and provides structured
// access to application settings including server port, authentication directory,
// debug settings, proxy configuration, and API keys.
package config

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// SDKConfig represents the application's configuration, loaded from a YAML file.
type SDKConfig struct {
	// ProxyURL is the URL of an optional proxy server to use for outbound requests.
	ProxyURL string `yaml:"proxy-url" json:"proxy-url"`

	// ForceModelPrefix requires explicit model prefixes (e.g., "teamA/gemini-3-pro-preview")
	// to target prefixed credentials. When false, unprefixed model requests may use prefixed
	// credentials as well.
	ForceModelPrefix bool `yaml:"force-model-prefix" json:"force-model-prefix"`

	// RequestLog enables or disables detailed request logging functionality.
	RequestLog bool `yaml:"request-log" json:"request-log"`

	// APIKeys is a list of keys for authenticating clients to this proxy server.
	// Supports both simple string keys and structured APIKeyEntry objects.
	APIKeys []APIKeyEntry `yaml:"api-keys" json:"api-keys"`

	// Access holds request authentication provider configuration.
	Access AccessConfig `yaml:"auth,omitempty" json:"auth,omitempty"`

	// Streaming configures server-side streaming behavior (keep-alives and safe bootstrap retries).
	Streaming StreamingConfig `yaml:"streaming" json:"streaming"`

	// NonStreamKeepAliveInterval controls how often blank lines are emitted for non-streaming responses.
	// <= 0 disables keep-alives. Value is in seconds.
	NonStreamKeepAliveInterval int `yaml:"nonstream-keepalive-interval,omitempty" json:"nonstream-keepalive-interval,omitempty"`
}

// APIKeyEntry represents an API key configuration with optional model restrictions.
// It supports both simple string format and structured object format in YAML.
type APIKeyEntry struct {
	// Key is the API key string.
	Key string `yaml:"key" json:"key"`

	// AllowedModels is an optional list of model patterns this key can access.
	// If empty, the key can access all models.
	// Supports wildcards: "gemini-*" matches "gemini-2.5-pro", "*-preview" matches "gemini-3-pro-preview".
	AllowedModels []string `yaml:"allowed-models,omitempty" json:"allowed-models,omitempty"`

	// DeniedModels is an optional list of model patterns this key cannot access.
	// Applied after AllowedModels. Supports the same wildcard patterns.
	DeniedModels []string `yaml:"denied-models,omitempty" json:"denied-models,omitempty"`
}

// UnmarshalYAML implements custom YAML unmarshaling to support both string and object formats.
func (e *APIKeyEntry) UnmarshalYAML(value *yaml.Node) error {
	// Try to unmarshal as a simple string first
	if value.Kind == yaml.ScalarNode {
		e.Key = value.Value
		e.AllowedModels = nil
		e.DeniedModels = nil
		return nil
	}

	// Otherwise, unmarshal as a structured object
	type rawEntry APIKeyEntry
	var raw rawEntry
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*e = APIKeyEntry(raw)
	return nil
}

// StreamingConfig holds server streaming behavior configuration.
type StreamingConfig struct {
	// KeepAliveSeconds controls how often the server emits SSE heartbeats (": keep-alive\n\n").
	// <= 0 disables keep-alives. Default is 0.
	KeepAliveSeconds int `yaml:"keepalive-seconds,omitempty" json:"keepalive-seconds,omitempty"`

	// BootstrapRetries controls how many times the server may retry a streaming request before any bytes are sent,
	// to allow auth rotation / transient recovery.
	// <= 0 disables bootstrap retries. Default is 0.
	BootstrapRetries int `yaml:"bootstrap-retries,omitempty" json:"bootstrap-retries,omitempty"`
}

// AccessConfig groups request authentication providers.
type AccessConfig struct {
	// Providers lists configured authentication providers.
	Providers []AccessProvider `yaml:"providers,omitempty" json:"providers,omitempty"`
}

// AccessProvider describes a request authentication provider entry.
type AccessProvider struct {
	// Name is the instance identifier for the provider.
	Name string `yaml:"name" json:"name"`

	// Type selects the provider implementation registered via the SDK.
	Type string `yaml:"type" json:"type"`

	// SDK optionally names a third-party SDK module providing this provider.
	SDK string `yaml:"sdk,omitempty" json:"sdk,omitempty"`

	// APIKeys lists inline keys for providers that require them.
	// Deprecated: Use APIKeyEntries for structured key configuration with model permissions.
	APIKeys []string `yaml:"api-keys,omitempty" json:"api-keys,omitempty"`

	// APIKeyEntries lists structured API key configurations with optional model restrictions.
	APIKeyEntries []APIKeyEntry `yaml:"-" json:"-"`

	// Config passes provider-specific options to the implementation.
	Config map[string]any `yaml:"config,omitempty" json:"config,omitempty"`
}

const (
	// AccessProviderTypeConfigAPIKey is the built-in provider validating inline API keys.
	AccessProviderTypeConfigAPIKey = "config-api-key"

	// DefaultAccessProviderName is applied when no provider name is supplied.
	DefaultAccessProviderName = "config-inline"
)

// ConfigAPIKeyProvider returns the first inline API key provider if present.
func (c *SDKConfig) ConfigAPIKeyProvider() *AccessProvider {
	if c == nil {
		return nil
	}
	for i := range c.Access.Providers {
		if c.Access.Providers[i].Type == AccessProviderTypeConfigAPIKey {
			if c.Access.Providers[i].Name == "" {
				c.Access.Providers[i].Name = DefaultAccessProviderName
			}
			return &c.Access.Providers[i]
		}
	}
	return nil
}

// MakeInlineAPIKeyProvider constructs an inline API key provider configuration.
// It returns nil when no keys are supplied.
func MakeInlineAPIKeyProvider(keys []string) *AccessProvider {
	if len(keys) == 0 {
		return nil
	}
	provider := &AccessProvider{
		Name:    DefaultAccessProviderName,
		Type:    AccessProviderTypeConfigAPIKey,
		APIKeys: append([]string(nil), keys...),
	}
	return provider
}

// MakeInlineAPIKeyProviderFromEntries constructs an inline API key provider configuration
// from structured APIKeyEntry objects that may include model permissions.
func MakeInlineAPIKeyProviderFromEntries(entries []APIKeyEntry) *AccessProvider {
	if len(entries) == 0 {
		return nil
	}
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Key != "" {
			keys = append(keys, entry.Key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	provider := &AccessProvider{
		Name:          DefaultAccessProviderName,
		Type:          AccessProviderTypeConfigAPIKey,
		APIKeys:       keys,
		APIKeyEntries: append([]APIKeyEntry(nil), entries...),
	}
	return provider
}

// GetAPIKeyStrings returns just the key strings from APIKeys entries.
func (c *SDKConfig) GetAPIKeyStrings() []string {
	if c == nil {
		return nil
	}
	keys := make([]string, 0, len(c.APIKeys))
	for _, entry := range c.APIKeys {
		if key := strings.TrimSpace(entry.Key); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

