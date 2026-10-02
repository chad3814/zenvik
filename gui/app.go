package main

import "context"

// App is the object bound to the frontend.
type App struct {
	ctx context.Context
}

// NewApp returns an App; Wails calls startup once the window exists.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// Version is the build's version string.
func (a *App) Version() string { return version }
