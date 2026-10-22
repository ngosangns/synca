package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	ExitGeneric      = 1
	ExitUsage        = 2
	ExitUnavailable  = 3
	ExitProtocol     = 4
	ExitFailed       = 5
	ExitNeedsConfirm = 6
	ExitUnauthorized = 7
	ExitConflict     = 8
)

// ExitError is a failure whose process status is Code.
// JSON means an error document was already written to stdout, so the caller
// must not also print the plain "synca failed" line.
// Quiet means the result was already printed and only the status matters
// (doctor and skill check when they find drift).
type ExitError struct {
	Code  int
	Msg   string
	Hint  string
	JSON  bool
	Quiet bool
}

func (e *ExitError) Error() string { return e.Msg }

// ExitCode maps an error to a process status. Plain errors stay 1.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	return 1
}

// Finish reports err the way the process entry does and returns the status.
func Finish(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var exit *ExitError
	if errors.As(err, &exit) && (exit.JSON || exit.Quiet) {
		return exit.Code
	}
	fmt.Fprintf(stderr, "synca failed: %v\n", err)
	if exit != nil && exit.Hint != "" {
		fmt.Fprintf(stderr, "hint: %s\n", exit.Hint)
	}
	return ExitCode(err)
}

type errorDocument struct {
	Error errorObject `json:"error"`
}

type errorObject struct {
	Code    string `json:"code"`
	Exit    int    `json:"exit"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func writeJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// writeJSONLine prints one JSON document on a single line. Error documents use
// this so a caller that shows the first line of stdout still sees the message.
func writeJSONLine(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func codeName(code int) string {
	switch code {
	case ExitUsage:
		return "usage"
	case ExitUnavailable:
		return "unavailable"
	case ExitProtocol:
		return "protocol"
	case ExitFailed:
		return "failed"
	case ExitNeedsConfirm:
		return "needs-confirm"
	case ExitUnauthorized:
		return "unauthorized"
	case ExitConflict:
		return "conflict"
	default:
		return "failed-generic"
	}
}

// failCode writes the v1 error object when jsonOut is set.
// hint is stored for the text path and omitted from JSON when empty.
func failCode(jsonOut bool, code int, message, hint string) error {
	if jsonOut {
		obj := errorObject{Code: codeName(code), Exit: code, Message: message, Hint: hint}
		if err := writeJSONLine(errorDocument{Error: obj}); err != nil {
			return err
		}
	}
	return &ExitError{Code: code, Msg: message, Hint: hint, JSON: jsonOut}
}

// failUsage is a usage error. With jsonOut it writes the v1 error object to stdout.
func failUsage(jsonOut bool, message, hint string) error {
	return failCode(jsonOut, ExitUsage, message, hint)
}

func failNeedsConfirm(jsonOut bool, message, hint string) error {
	return failCode(jsonOut, ExitNeedsConfirm, message, hint)
}

// statusOnly returns a process status without a "synca failed" line.
func statusOnly(code int) error {
	return &ExitError{Code: code, Quiet: true}
}
