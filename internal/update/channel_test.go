package update

import "testing"

func TestDetect(t *testing.T) {
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	cases := []struct {
		name      string
		p         Product
		exe       string
		dirExists func(string) bool
		wantName  string
		wantCmd   string
		wantKey   string
		wantCache string
	}{
		{"brew formula arm", CLI, "/opt/homebrew/Cellar/zenvik/1.2.1/bin/zenvik", no, "Homebrew", "brew upgrade zenvik", "github", "update-check.json"},
		{"brew formula intel", CLI, "/usr/local/Cellar/zenvik/1.2.1/bin/zenvik", no, "Homebrew", "brew upgrade zenvik", "github", "update-check.json"},
		{"scoop user", CLI, `C:\Users\chad\scoop\apps\zenvik\1.2.1\zenvik.exe`, no, "Scoop", "scoop update zenvik", "github", "update-check.json"},
		{"scoop current junction", CLI, `C:\Users\chad\scoop\apps\zenvik\current\zenvik.exe`, no, "Scoop", "scoop update zenvik", "github", "update-check.json"},
		{"scoop global gui", GUI, `C:\ProgramData\scoop\apps\zenvik-gui\1.2.1\zenvik-gui.exe`, no, "Scoop", "scoop update zenvik-gui", "github", "update-check.json"},
		{"winget cli", CLI, `C:\Users\chad\AppData\Local\Microsoft\WinGet\Packages\chad3814.Zenvik_Microsoft.Winget.Source_8wekyb3d8bbwe\zenvik_1.2.1_windows_amd64\zenvik.exe`, no, "winget", "winget upgrade chad3814.Zenvik", "winget:chad3814.Zenvik", "update-check-winget-chad3814.Zenvik.json"},
		{"winget gui", GUI, `C:\Users\chad\AppData\Local\Microsoft\WinGet\Packages\chad3814.ZenvikGUI_Microsoft.Winget.Source_8wekyb3d8bbwe\zenvik-gui_1.2.1_windows_amd64\zenvik-gui.exe`, no, "winget", "winget upgrade chad3814.ZenvikGUI", "winget:chad3814.ZenvikGUI", "update-check-winget-chad3814.ZenvikGUI.json"},
		{"choco cli", CLI, `C:\ProgramData\chocolatey\lib\zenvik\tools\zenvik_1.2.1_windows_amd64\zenvik.exe`, no, "Chocolatey", "choco upgrade zenvik", "chocolatey:zenvik", "update-check-chocolatey-zenvik.json"},
		{"choco gui", GUI, `C:\ProgramData\chocolatey\lib\zenvik-gui\tools\zenvik-gui_1.2.1_windows_amd64\zenvik-gui.exe`, no, "Chocolatey", "choco upgrade zenvik-gui", "chocolatey:zenvik-gui", "update-check-chocolatey-zenvik-gui.json"},
		{"cask", GUI, "/Applications/Zenvik.app/Contents/MacOS/zenvik-gui", yes, "Homebrew", "brew upgrade --cask zenvik-gui", "github", "update-check.json"},
		{"app without caskroom", GUI, "/Applications/Zenvik.app/Contents/MacOS/zenvik-gui", no, "", "", "github", "update-check.json"},
		{"app with nil dirExists", GUI, "/Applications/Zenvik.app/Contents/MacOS/zenvik-gui", nil, "", "", "github", "update-check.json"},
		{"direct unix", CLI, "/usr/local/bin/zenvik", no, "", "", "github", "update-check.json"},
		{"direct windows", CLI, `C:\Tools\zenvik\zenvik.exe`, no, "", "", "github", "update-check.json"},
		{"empty", CLI, "", yes, "", "", "github", "update-check.json"},
		{"cli in gui package", CLI, `C:\ProgramData\chocolatey\lib\zenvik-gui\tools\zenvik-gui_1.2.1_windows_amd64\zenvik.exe`, no, "", "", "github", "update-check.json"},
		{"gui in cli package", GUI, `C:\ProgramData\chocolatey\lib\zenvik\tools\zenvik_1.2.1_windows_amd64\zenvik-gui.exe`, no, "", "", "github", "update-check.json"},
		{"cli does not use cask rule", CLI, "/Applications/Zenvik.app/Contents/MacOS/zenvik", yes, "", "", "github", "update-check.json"},
		{"mixed case", CLI, `c:\programdata\CHOCOLATEY\LIB\Zenvik\tools\x\zenvik.exe`, no, "Chocolatey", "choco upgrade zenvik", "chocolatey:zenvik", "update-check-chocolatey-zenvik.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := Detect(tc.p, tc.exe, tc.dirExists)
			if ch.Name != tc.wantName || ch.Upgrade != tc.wantCmd {
				t.Errorf("Detect = %+v, want name %q, upgrade %q", ch, tc.wantName, tc.wantCmd)
			}
			if ch.Managed() != (tc.wantName != "") {
				t.Errorf("Managed() = %v for %+v", ch.Managed(), ch)
			}
			if ch.sourceKey() != tc.wantKey || ch.cacheName() != tc.wantCache {
				t.Errorf("source %q cache %q, want %q %q", ch.sourceKey(), ch.cacheName(), tc.wantKey, tc.wantCache)
			}
		})
	}
}

func TestCaskPrefixes(t *testing.T) {
	var asked []string
	exists := func(p string) bool {
		asked = append(asked, p)
		return p == "/usr/local/Caskroom/zenvik-gui"
	}
	ch := Detect(GUI, "/Applications/Zenvik.app/Contents/MacOS/zenvik-gui", exists)
	if ch.Name != "Homebrew" {
		t.Errorf("Detect = %+v, want the cask under /usr/local", ch)
	}
	if len(asked) != 2 || asked[0] != "/opt/homebrew/Caskroom/zenvik-gui" || asked[1] != "/usr/local/Caskroom/zenvik-gui" {
		t.Errorf("asked %v, want both prefixes in order", asked)
	}
}

func TestDetectSelfIsDirectInTests(t *testing.T) {
	// The test binary lives in a temp build dir: never a managed location.
	if ch := DetectSelf(CLI); ch.Managed() {
		t.Errorf("DetectSelf = %+v, want direct", ch)
	}
	if ch := DetectSelf(GUI); ch.Managed() {
		t.Errorf("DetectSelf(GUI) = %+v, want direct", ch)
	}
}
