package cli

// ExitError makes the program exit with a specific code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }

// Unwrap returns the underlying error.
func (e *ExitError) Unwrap() error { return e.Err }

// ExitCodeNeedsAdmin is returned when an action needs administrator rights.
const ExitCodeNeedsAdmin = 3
