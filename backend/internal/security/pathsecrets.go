package security

import (
	"path/filepath"
	"strings"
)

// BlockAgentReadPath reports whether a workspace-relative path must not be read by the agent (contains local secrets).
func BlockAgentReadPath(rel string) bool {
	return blockClawmindConfig(rel)
}

// BlockAgentWritePath reports whether a workspace-relative path must not be written by the agent.
func BlockAgentWritePath(rel string) bool {
	return blockClawmindConfig(rel)
}

func blockClawmindConfig(rel string) bool {
	rel = filepath.ToSlash(filepath.Clean(strings.TrimSpace(rel)))
	if rel == "." || rel == "" {
		return false
	}
	// Any .../.clawmind/config.json under workspace
	if rel == ".clawmind/config.json" || strings.HasSuffix(rel, "/.clawmind/config.json") {
		return true
	}
	return false
}
