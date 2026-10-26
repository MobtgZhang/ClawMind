package security

// CommandDenied reports commands that are never executed, even with user approval, under the
// default policy. A patch layer adds patterns to this floor; it cannot remove one.
func CommandDenied(command string) bool {
	return DefaultPolicy().CommandDenied(command)
}

// CommandHighRisk reports commands that need explicit user approval under the default policy.
func CommandHighRisk(command string) bool {
	return DefaultPolicy().CommandHighRisk(command)
}
