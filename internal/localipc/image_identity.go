package localipc

// ExecutableCodeID returns the same executable code identity format used by
// ProcessIdentity.CodeID. The path must name a regular executable file that the
// platform can validate; a filename or self-reported label is never enough.
func ExecutableCodeID(path string) (string, error) {
	if path == "" {
		return "", ErrIdentityUnavailable
	}
	return executableCodeID(path)
}
