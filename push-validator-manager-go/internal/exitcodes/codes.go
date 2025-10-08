package exitcodes

import (
	"fmt"
	"os"
)

// Standard exit codes for push-validator-manager
const (
	// Success indicates successful command completion
	Success = 0

	// GeneralError indicates a general/unknown error
	GeneralError = 1

	// InvalidArgs indicates invalid command-line arguments or flags
	InvalidArgs = 2

	// PreconditionFailed indicates a precondition was not met
	// (e.g., node not initialized, missing config, already running)
	PreconditionFailed = 3

	// NetworkError indicates network/connectivity failure
	// (e.g., RPC unreachable, timeout, DNS failure)
	NetworkError = 4

	// ProcessError indicates process management failure
	// (e.g., failed to start/stop, permission denied)
	ProcessError = 5

	// ValidationError indicates validation failure
	// (e.g., invalid config, corrupted data)
	ValidationError = 6
)

// Exit terminates the program with the given code
func Exit(code int) {
	os.Exit(code)
}

// ExitWithError prints error message to stderr and exits with the given code
func ExitWithError(code int, msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(code)
}

// ExitWithErrorf prints formatted error message to stderr and exits
func ExitWithErrorf(code int, format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}

// CodeForError returns the appropriate exit code for an error.
// Unwraps ErrorWithCode for explicit codes, otherwise returns GeneralError.
// Use explicit error constructors (NetworkErr, ProcessErr, etc.) for specific codes.
func CodeForError(err error) int {
	if err == nil {
		return Success
	}

	// Check if error has explicit code
	if ec, ok := err.(*ErrorWithCode); ok {
		return ec.Code
	}

	// Default to general error - callers should use explicit error constructors
	return GeneralError
}
