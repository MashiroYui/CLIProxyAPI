package access

import (
	"encoding/json"
	"strings"
)

// ModelPermissionMetadataKey is the key used in access metadata to store model permissions.
const ModelPermissionMetadataKey = "model_permission"

// ModelPermission holds the model access rules for an API key.
type ModelPermission struct {
	AllowedModels []string
	DeniedModels  []string
}

// CanAccessModel checks if the given model is allowed based on the permission rules.
// If AllowedModels is empty, all models are allowed unless explicitly denied.
// If AllowedModels is non-empty, only matching models are allowed.
// DeniedModels is always checked and takes precedence.
//
// If the model contains routing prefixes (e.g. "teamA/gemini-2.5-pro"), the rules are
// evaluated against both the full name and the suffix after the last slash.
func (p *ModelPermission) CanAccessModel(model string) bool {
	if p == nil {
		return true
	}

	names := candidateModelNames(model)
	if len(names) == 0 {
		return true
	}

	// Denied list takes precedence.
	for _, pattern := range p.DeniedModels {
		for _, name := range names {
			if matchModelPattern(pattern, name) {
				return false
			}
		}
	}

	// If no allowed list, all models are allowed (except denied ones).
	if len(p.AllowedModels) == 0 {
		return true
	}

	for _, pattern := range p.AllowedModels {
		for _, name := range names {
			if matchModelPattern(pattern, name) {
				return true
			}
		}
	}

	return false
}

func candidateModelNames(model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	out := []string{model}
	if idx := strings.LastIndex(model, "/"); idx >= 0 && idx+1 < len(model) {
		suffix := strings.TrimSpace(model[idx+1:])
		if suffix != "" && suffix != model {
			out = append(out, suffix)
		}
	}
	return out
}

// matchModelPattern checks if a model name matches a pattern.
// Patterns support:
//   - Exact match: "gemini-2.5-pro" matches "gemini-2.5-pro"
//   - Prefix wildcard: "gemini-*" matches "gemini-2.5-pro", "gemini-3-flash"
//   - Suffix wildcard: "*-preview" matches "gemini-3-pro-preview"
//   - Contains wildcard: "*flash*" matches "gemini-2.5-flash-lite"
//   - Single wildcard "*" matches everything
func matchModelPattern(pattern, model string) bool {
	pattern = strings.TrimSpace(pattern)
	model = strings.TrimSpace(model)

	if pattern == "" || model == "" {
		return false
	}

	// Exact match
	if pattern == model {
		return true
	}

	// Single wildcard matches everything
	if pattern == "*" {
		return true
	}

	// Check for wildcard patterns
	hasPrefix := strings.HasPrefix(pattern, "*")
	hasSuffix := strings.HasSuffix(pattern, "*")

	switch {
	case hasPrefix && hasSuffix:
		// Contains pattern: *flash*
		core := strings.Trim(pattern, "*")
		if core == "" {
			return true
		}
		return strings.Contains(model, core)

	case hasPrefix:
		// Suffix pattern: *-preview
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(model, suffix)

	case hasSuffix:
		// Prefix pattern: gemini-*
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(model, prefix)

	default:
		// No wildcards, already checked exact match
		return false
	}
}

// ParseModelPermission parses a ModelPermission from JSON string.
// Returns nil if the JSON is empty or invalid.
func ParseModelPermission(jsonStr string) *ModelPermission {
	if jsonStr == "" {
		return nil
	}
	var perm ModelPermission
	if err := json.Unmarshal([]byte(jsonStr), &perm); err != nil {
		return nil
	}
	return &perm
}

// CheckModelAccessFromMetadata checks if a model is allowed based on the model permission
// stored in the access metadata. Returns true if no permission is configured or if the model is allowed.
func CheckModelAccessFromMetadata(metadata map[string]string, model string) bool {
	if metadata == nil {
		return true
	}
	permJSON, ok := metadata[ModelPermissionMetadataKey]
	if !ok || permJSON == "" {
		return true
	}
	perm := ParseModelPermission(permJSON)
	return perm.CanAccessModel(model)
}
