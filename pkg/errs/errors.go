package errs

import (
	"fmt"
	"net"
)

type Code string

const (
	CodeUnknown        Code = "UNKNOWN"
	CodeNotFound       Code = "NOT_FOUND"
	CodeAlreadyExists  Code = "ALREADY_EXISTS"
	CodeUnauthorized   Code = "UNAUTHORIZED"
	CodeForbidden      Code = "FORBIDDEN"
	CodeTimeout        Code = "TIMEOUT"
	CodeUnavailable    Code = "UNAVAILABLE"
	CodeInternal       Code = "INTERNAL"
	CodeCanceled       Code = "CANCELED"
	CodeDeadline       Code = "DEADLINE_EXCEEDED"
	CodeInvalidArg     Code = "INVALID_ARGUMENT"
	CodeConnectionLost Code = "CONNECTION_LOST"
)

type Error struct {
	Code    Code
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func Wrap(code Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}

func WithCode(code Code, cause error) *Error {
	return &Error{Code: code, Message: cause.Error(), Cause: cause}
}

func GetCode(err error) Code {
	if te, ok := err.(*Error); ok {
		return te.Code
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return GetCode(wrapped.Unwrap())
	}
	return CodeUnknown
}

func IsNotFound(err error) bool       { return GetCode(err) == CodeNotFound }
func IsUnauthorized(err error) bool   { return GetCode(err) == CodeUnauthorized }
func IsUnavailable(err error) bool    { return GetCode(err) == CodeUnavailable }
func IsTimeout(err error) bool        { return GetCode(err) == CodeTimeout }
func IsCanceled(err error) bool       { return GetCode(err) == CodeCanceled }
func IsConnectionLost(err error) bool { return GetCode(err) == CodeConnectionLost }

func IsTemporary(err error) bool {
	if ne, ok := err.(interface{ Temporary() bool }); ok {
		return ne.Temporary()
	}
	if ne, ok := err.(net.Error); ok {
		return ne.Timeout()
	}
	return false
}
