package domain

import (
	"fmt"
	"regexp"
	"tunneledge/pkg/errs"
)

var (
	labelRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)
	// emailRegexp is a pragmatic RFC 5321 subset — rejects obvious garbage while
	// remaining permissive enough for real-world addresses.
	emailRegexp = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
)

func ValidateLabel(label string) error {
	if label == "" {
		return errs.New(errs.CodeInvalidArg, "label cannot be empty")
	}
	if len(label) > 64 {
		return errs.New(errs.CodeInvalidArg, fmt.Sprintf("label too long: %d chars (max 64)", len(label)))
	}
	if !labelRegexp.MatchString(label) {
		return errs.New(errs.CodeInvalidArg, fmt.Sprintf("label %q is not a valid DNS-safe name (lowercase alphanumeric and hyphens only)", label))
	}
	return nil
}

func ValidateLocalAddr(addr string) error {
	if addr == "" {
		return errs.New(errs.CodeInvalidArg, "local_addr cannot be empty")
	}
	if len(addr) > 256 {
		return errs.New(errs.CodeInvalidArg, "local_addr too long")
	}
	return nil
}

// ValidateEmail checks that email is a plausible RFC 5321 address.
func ValidateEmail(email string) error {
	if email == "" {
		return errs.New(errs.CodeInvalidArg, "email cannot be empty")
	}
	if len(email) > 254 {
		return errs.New(errs.CodeInvalidArg, "email too long (max 254 chars)")
	}
	if !emailRegexp.MatchString(email) {
		return errs.New(errs.CodeInvalidArg, fmt.Sprintf("email %q is not a valid address", email))
	}
	return nil
}
