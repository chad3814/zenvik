package update

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	wingetContents    = "https://api.github.com/repos/microsoft/winget-pkgs/contents/manifests/"
	chocolateyFeedURL = "https://community.chocolatey.org/api/v2/"
)

// wingetDir is the manifests path for a package id: "chad3814.Zenvik" →
// "c/chad3814/Zenvik".
func wingetDir(pkg string) string {
	publisher, name, _ := strings.Cut(pkg, ".")
	return strings.ToLower(publisher[:1]) + "/" + publisher + "/" + name
}

// get issues the request with the user agent and the extra headers.
func (c *Checker) get(ctx context.Context, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return c.client().Do(req)
}

var githubHeaders = map[string]string{"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"}

// fetchWinget lists the package's manifest directory in microsoft/winget-pkgs;
// each published version is a directory named after it.
func (c *Checker) fetchWinget(ctx context.Context) (Version, string, error) {
	resp, err := c.get(ctx, githubHeaders)
	if err != nil {
		return Version{}, "", err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Version{}, "", errors.New(c.Channel.pkg + " is not on winget yet")
	case resp.StatusCode != http.StatusOK:
		return Version{}, "", errors.New("HTTP " + resp.Status)
	}
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&entries); err != nil {
		return Version{}, "", fmt.Errorf("invalid response: %w", err)
	}
	var best Version
	found := false
	for _, e := range entries {
		if e.Type != "dir" {
			continue
		}
		v, err := Parse("v" + e.Name)
		if err != nil {
			continue
		}
		if !found || best.Less(v) {
			best, found = v, true
		}
	}
	if !found {
		return Version{}, "", errors.New("no versions on winget")
	}
	return best, "", nil
}

// fetchChocolatey reads the package's versions from the community feed and
// returns the highest approved one; a version still in moderation is never
// offered.
func (c *Checker) fetchChocolatey(ctx context.Context) (Version, string, error) {
	resp, err := c.get(ctx, map[string]string{"Accept": "application/atom+xml"})
	if err != nil {
		return Version{}, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Version{}, "", errors.New("HTTP " + resp.Status)
	}
	var feed struct {
		Entries []struct {
			Props struct {
				Version    string `xml:"Version"`
				IsApproved string `xml:"IsApproved"`
			} `xml:"properties"`
		} `xml:"entry"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&feed); err != nil {
		return Version{}, "", fmt.Errorf("invalid response: %w", err)
	}
	if len(feed.Entries) == 0 {
		return Version{}, "", errors.New(c.Channel.pkg + " is not on Chocolatey yet")
	}
	var best Version
	found := false
	for _, e := range feed.Entries {
		if !strings.EqualFold(strings.TrimSpace(e.Props.IsApproved), "true") {
			continue
		}
		v, err := Parse("v" + strings.TrimSpace(e.Props.Version))
		if err != nil {
			continue
		}
		if !found || best.Less(v) {
			best, found = v, true
		}
	}
	if !found {
		return Version{}, "", errors.New("no approved version on Chocolatey")
	}
	return best, "", nil
}
