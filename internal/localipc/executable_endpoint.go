package localipc

import "errors"

type executableDialError struct {
	err error
}

func (e *executableDialError) Error() string {
	return e.err.Error()
}

func (e *executableDialError) Unwrap() error {
	return e.err
}

func IsExecutableEndpointUnavailable(err error) bool {
	var dialErr *executableDialError
	if !errors.As(err, &dialErr) || dialErr == nil {
		return false
	}
	return executableEndpointUnavailable(dialErr.err)
}
