package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	// DefaultURL is GitHub's "latest final release" endpoint for zenvik.
	DefaultURL = "https://api.github.com/repos/chad3814/zenvik/releases/latest"
	// LatestPage is the human-facing page the notices link to.
	LatestPage = "https://github.com/chad3814/zenvik/releases/latest"

	cacheTTL       = 24 * time.Hour
	maxBody        = 1 << 20
	defaultTimeout = 5 * time.Second
)

var (
	// ErrDevBuild says the running version is not a release, so nothing was
	// checked.
	ErrDevBuild = errors.New("zenvik: update check skipped in development builds")
	// ErrFetch says the latest release could not be determined; the message
	// carries the detail.
	ErrFetch = errors.New("zenvik: update check failed")
)

// Checker finds the newest final release of zenvik. The zero value uses
// GitHub, a 5 s timeout, DefaultCachePath and time.Now.
type Checker struct {
	Client    *http.Client     // nil: http.Client with a 5 s timeout
	URL       string           // "": DefaultURL
	CachePath string           // "": DefaultCachePath()
	Now       func() time.Time // nil: time.Now
	UserAgent string           // "": "zenvik"
}

// Result is what a check learned.
type Result struct {
	Current   Version
	Latest    Version
	URL       string    // the release's page (html_url)
	Newer     bool      // Current.Less(Latest)
	CheckedAt time.Time // when Latest was fetched (zero when it never was)
}

// cacheFile is update-check.json. Newer is never stored: it is recomputed
// against whichever version reads the file.
type cacheFile struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
	URL       string    `json:"url"`
	Error     string    `json:"error"`
}

// Check returns the cached result when it is less than 24 hours old,
// otherwise fetches, records the attempt in the cache and returns.
// A current version that is not a release tag returns ErrDevBuild.
func (c *Checker) Check(ctx context.Context, current string) (Result, error) {
	cur, err := Parse(current)
	if err != nil {
		return Result{}, ErrDevBuild
	}
	now := c.now()
	if cf, ok := c.readCache(); ok && !cf.CheckedAt.After(now) && now.Sub(cf.CheckedAt) < cacheTTL {
		return cf.result(cur)
	}
	return c.fetchAndRecord(ctx, cur)
}

// Force always fetches, then records the attempt in the cache.
func (c *Checker) Force(ctx context.Context, current string) (Result, error) {
	cur, err := Parse(current)
	if err != nil {
		return Result{}, ErrDevBuild
	}
	return c.fetchAndRecord(ctx, cur)
}

func (c *Checker) fetchAndRecord(ctx context.Context, cur Version) (Result, error) {
	cf := cacheFile{CheckedAt: c.now().UTC()}
	latest, url, err := c.fetch(ctx)
	switch {
	case errors.Is(err, context.Canceled) && !errors.Is(context.Cause(ctx), context.DeadlineExceeded):
		// The caller gave up (Ctrl-C); that says nothing about the network.
		// A cancellation whose cause wraps DeadlineExceeded is a timeout
		// and is recorded below.
		return Result{Current: cur}, fmt.Errorf("%w: %w", ErrFetch, err)
	case err != nil:
		cf.Error = err.Error()
	default:
		cf.Latest, cf.URL = latest.String(), url
	}
	c.writeCache(cf)
	return cf.result(cur)
}

// result turns a cache entry into what cur's caller wants to know.
func (cf cacheFile) result(cur Version) (Result, error) {
	r := Result{Current: cur, CheckedAt: cf.CheckedAt}
	if cf.Error != "" {
		return r, fmt.Errorf("%w: %s", ErrFetch, cf.Error)
	}
	latest, err := Parse(cf.Latest)
	if err != nil {
		return r, fmt.Errorf("%w: cached tag %q", ErrFetch, cf.Latest)
	}
	r.Latest, r.URL, r.Newer = latest, cf.URL, cur.Less(latest)
	return r, nil
}

// fetch asks the API for the latest release. Its errors are the detail
// only; callers wrap ErrFetch.
func (c *Checker) fetch(ctx context.Context) (Version, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(), nil)
	if err != nil {
		return Version{}, "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.userAgent())
	resp, err := c.client().Do(req)
	if err != nil {
		return Version{}, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Version{}, "", errors.New("HTTP " + resp.Status)
	}
	var body struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return Version{}, "", fmt.Errorf("invalid response: %w", err)
	}
	latest, err := Parse(body.Tag)
	if err != nil {
		return Version{}, "", fmt.Errorf("unexpected release tag %q", body.Tag)
	}
	return latest, body.URL, nil
}

func (c *Checker) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: defaultTimeout}
}

func (c *Checker) url() string {
	if c.URL != "" {
		return c.URL
	}
	return DefaultURL
}

func (c *Checker) userAgent() string {
	if c.UserAgent != "" {
		return c.UserAgent
	}
	return "zenvik"
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) cachePath() (string, error) {
	if c.CachePath != "" {
		return c.CachePath, nil
	}
	return DefaultCachePath()
}

// readCache returns the cache entry, or false when there is none usable.
func (c *Checker) readCache() (cacheFile, bool) {
	p, err := c.cachePath()
	if err != nil {
		return cacheFile{}, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return cacheFile{}, false
	}
	var cf cacheFile
	if json.Unmarshal(b, &cf) != nil || cf.CheckedAt.IsZero() {
		return cacheFile{}, false
	}
	if cf.Error == "" {
		if _, err := Parse(cf.Latest); err != nil {
			return cacheFile{}, false
		}
	}
	return cf, true
}

// writeCache replaces the cache atomically. Failures are ignored: the next
// run simply fetches again.
func (c *Checker) writeCache(cf cacheFile) {
	p, err := c.cachePath()
	if err != nil {
		return
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	b, err := json.Marshal(cf)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".update-check-*")
	if err != nil {
		return
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		_ = os.Remove(tmp.Name())
	}
}

// DefaultCachePath is $XDG_STATE_HOME/zenvik/update-check.json when
// XDG_STATE_HOME is absolute, else the user cache directory's
// zenvik/update-check.json (the roots the mount records and the GUI queue
// already use).
func DefaultCachePath() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "zenvik", "update-check.json"), nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "zenvik", "update-check.json"), nil
}
