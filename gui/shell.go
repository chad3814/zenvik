package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Shell is what App needs from the window: events and dialogs. wailsShell is
// the real one; tests use a fake.
type Shell interface {
	Emit(event string, data any)
	PickFiles(title, filterName, pattern string) ([]string, error)
	PickDir(title string) (string, error)
	Confirm(title, message, yes, no string) (bool, error)
}

type wailsShell struct{ ctx context.Context }

func (s wailsShell) Emit(event string, data any) { runtime.EventsEmit(s.ctx, event, data) }

func (s wailsShell) PickFiles(title, filterName, pattern string) ([]string, error) {
	return runtime.OpenMultipleFilesDialog(s.ctx, runtime.OpenDialogOptions{
		Title:   title,
		Filters: []runtime.FileFilter{{DisplayName: filterName, Pattern: pattern}},
	})
}

func (s wailsShell) PickDir(title string) (string, error) {
	return runtime.OpenDirectoryDialog(s.ctx, runtime.OpenDialogOptions{Title: title, CanCreateDirectories: true})
}

// Confirm asks a yes/no question. Windows dialogs ignore custom buttons and
// answer "Yes" or "No", so "Yes" counts as yes too.
func (s wailsShell) Confirm(title, message, yes, no string) (bool, error) {
	r, err := runtime.MessageDialog(s.ctx, runtime.MessageDialogOptions{
		Type: runtime.QuestionDialog, Title: title, Message: message,
		Buttons: []string{yes, no}, DefaultButton: no, CancelButton: no,
	})
	return r == yes || r == "Yes", err
}
