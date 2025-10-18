package exitcodes

import "fmt"

// ErrorWithCode is an error that carries an explicit exit code
type ErrorWithCode struct {
	Code    int
	Message string
	Cause   error
}

func (e *ErrorWithCode) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *ErrorWithCode) Unwrap() error {
	return e.Cause
}

// NewError creates an error with an explicit exit code
func NewError(code int, message string) *ErrorWithCode {
	return &ErrorWithCode{Code: code, Message: message}
}

// NewErrorf creates an error with formatted message and exit code
func NewErrorf(code int, format string, args ...interface{}) *ErrorWithCode {
	return &ErrorWithCode{Code: code, Message: fmt.Sprintf(format, args...)}
}

// WrapError wraps an existing error with an exit code
func WrapError(code int, message string, cause error) *ErrorWithCode {
	return &ErrorWithCode{Code: code, Message: message, Cause: cause}
}

// Common error constructors

func InvalidArgsError(message string) *ErrorWithCode {
	return NewError(InvalidArgs, message)
}

func InvalidArgsErrorf(format string, args ...interface{}) *ErrorWithCode {
	return NewErrorf(InvalidArgs, format, args...)
}

func PreconditionError(message string) *ErrorWithCode {
	return NewError(PreconditionFailed, message)
}

func PreconditionErrorf(format string, args ...interface{}) *ErrorWithCode {
	return NewErrorf(PreconditionFailed, format, args...)
}

func NetworkErr(message string) *ErrorWithCode {
	return NewError(NetworkError, message)
}

func NetworkErrf(format string, args ...interface{}) *ErrorWithCode {
	return NewErrorf(NetworkError, format, args...)
}

func ProcessErr(message string) *ErrorWithCode {
	return NewError(ProcessError, message)
}

func ProcessErrf(format string, args ...interface{}) *ErrorWithCode {
	return NewErrorf(ProcessError, format, args...)
}

func ValidationErr(message string) *ErrorWithCode {
	return NewError(ValidationError, message)
}

func ValidationErrf(format string, args ...interface{}) *ErrorWithCode {
	return NewErrorf(ValidationError, format, args...)
}
