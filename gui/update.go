package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/chad3814/zenvik/gui/internal/errs"
	"github.com/chad3814/zenvik/internal/update"
)

// updateTimeout bounds each request to GitHub.
const updateTimeout = 10 * time.Second

// startUpdateCheck runs the cached update check in the background and
// raises the "update" banner when a newer release exists. It does nothing
// in development builds, when update_check is false in the config, when
// ZENVIK_NO_UPDATE_CHECK is set, or when no checker was supplied.
func (a *App) startUpdateCheck(ctx context.Context) {
	if _, err := update.Parse(version); err != nil || a.deps.CheckUpdate == nil {
		return
	}
	if os.Getenv("ZENVIK_NO_UPDATE_CHECK") != "" {
		return
	}
	a.mu.Lock()
	want := a.settings.UpdateCheck
	a.mu.Unlock()
	if !want {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(ctx, updateTimeout)
		defer cancel()
		r, err := a.deps.CheckUpdate(ctx, version)
		if err != nil || !r.Newer {
			return
		}
		a.setBanner(updateBanner(r))
	}()
}

func updateBanner(r update.Result) Banner {
	return Banner{ID: "update", Message: "Zenvik " + r.Latest.String() + " is available.", Action: "download", URL: update.LatestPage}
}

// CheckForUpdate asks GitHub now and returns the newer release's tag, or ""
// when this build is the latest. Development builds and network problems
// return an error whose message is shown as is.
func (a *App) CheckForUpdate() (string, error) {
	if !a.wait() {
		return "", errNotStarted
	}
	if a.deps.ForceUpdate == nil {
		return "", errors.New(errs.Message(update.ErrDevBuild))
	}
	ctx, cancel := context.WithTimeout(a.ctx, updateTimeout)
	defer cancel()
	r, err := a.deps.ForceUpdate(ctx, version)
	if err != nil {
		return "", errors.New(errs.Message(err))
	}
	if !r.Newer {
		return "", nil
	}
	return r.Latest.String(), nil
}
