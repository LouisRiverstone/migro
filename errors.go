package migro

import (
	"errors"
	"fmt"
)

// Code is a stable, machine-readable error identifier.
type Code string

const (
	UnsupportedDialect Code = "UNSUPPORTED_DIALECT"
	ConnectionFailed   Code = "CONNECTION_FAILED"
	NotFound           Code = "NOT_FOUND"
	MigrationLocked    Code = "MIGRATION_LOCKED"
	MigrationFailed    Code = "MIGRATION_FAILED"
	ChecksumMismatch   Code = "MIGRATION_CHECKSUM_MISMATCH"
	InvalidSchema      Code = "INVALID_SCHEMA"
	UnsupportedFeature Code = "UNSUPPORTED_FEATURE"
	QueryFailed        Code = "QUERY_FAILED"
	InvalidConfig      Code = "INVALID_CONFIG"
	PoolAcquireTimeout Code = "POOL_ACQUIRE_TIMEOUT"
	EventAborted       Code = "EVENT_ABORTED"
	EventHandlerFailed Code = "EVENT_HANDLER_FAILED"
	SeedFailed         Code = "SEED_FAILED"
	DiffFailed         Code = "DIFF_FAILED"
	ShardNotFound      Code = "SHARD_NOT_FOUND"
	ShardFailed        Code = "SHARD_FAILED"
	UnknownMorphType   Code = "UNKNOWN_MORPH_TYPE"
)

// Error is the single error type returned across migro boundaries.
// Match with errors.Is(err, migro.ErrNotFound) or errors.As.
type Error struct {
	Code    Code
	Message string
	Field   string
	Err     error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("migro: %s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("migro: %s: %s: %v", e.Code, e.Message, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// Is matches on Code so sentinels work with errors.Is.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// Sentinels for errors.Is.
var (
	ErrNotFound           = &Error{Code: NotFound, Message: "record not found"}
	ErrMigrationLocked    = &Error{Code: MigrationLocked}
	ErrUnsupportedFeature = &Error{Code: UnsupportedFeature}
	ErrPoolAcquireTimeout = &Error{Code: PoolAcquireTimeout}
	ErrEventAborted       = &Error{Code: EventAborted}
)

// Wrap builds an *Error around a cause.
func Wrap(code Code, cause error, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Err: cause}
}

// Newf builds an *Error without a cause.
func Newf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// CodeOf extracts the migro Code from err, or "" when err is not a migro error.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
