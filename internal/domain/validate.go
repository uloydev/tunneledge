package domain

import (
	"fmt"
	"regexp"
	"tunneledge/pkg/errs"
)

var labelRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

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
