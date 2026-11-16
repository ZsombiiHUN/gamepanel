package store

import (
	"fmt"
	"strings"
)

// UserError is a failure whose message is written for the person using the
// panel: bad input, a limit reached, a state that forbids the action. The API
// shows its message as is. Any other error is logged and replaced with a
// generic message, so database or system details never reach a browser.
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func userErr(msg string) error { return &UserError{Msg: msg} }

func userErrf(format string, a ...any) error { return &UserError{Msg: fmt.Sprintf(format, a...)} }

// uniqueErr turns a unique-constraint failure into msg for people; other
// errors pass through.
func uniqueErr(err error, msg string) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return userErr(msg)
	}
	return err
}
