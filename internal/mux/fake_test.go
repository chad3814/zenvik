package mux

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess is not a real test: when ZENVIK_FAKE_MKVMERGE is set,
// the test binary acts as mkvmerge so the package can be tested without
// MKVToolNix and on every OS.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("ZENVIK_FAKE_MKVMERGE")
	if mode == "" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	os.Exit(fakeMkvmerge(mode, args))
}

func fakeMkvmerge(mode string, args []string) int {
	if len(args) == 1 && strings.HasPrefix(args[0], "@") {
		b, err := os.ReadFile(args[0][1:])
		if err != nil {
			fmt.Println("Error: cannot read the options file")
			return 2
		}
		if err := json.Unmarshal(b, &args); err != nil {
			fmt.Println("Error: the options file is not a JSON array")
			return 2
		}
	}
	if p := os.Getenv("ZENVIK_FAKE_ARGS_OUT"); p != "" {
		b, _ := json.Marshal(args)
		_ = os.WriteFile(p, b, 0o644)
	}
	kind, value, _ := strings.Cut(mode, "=")
	switch kind {
	case "version":
		fmt.Println(value)
		return 0
	case "identify":
		b, err := os.ReadFile(value)
		if err != nil {
			return 2
		}
		_, _ = os.Stdout.Write(b)
		return 0
	case "mux", "hang":
		out := ""
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "-o" {
				out = args[i+1]
			}
		}
		fmt.Println("mkvmerge v102.0 ('Little Houses') 64-bit")
		fmt.Println("#GUI#begin_scanning_playlists#num_playlists=1#num_files_in_playlists=2")
		fmt.Println("#GUI#progress 50%")
		fmt.Println("#GUI#end_scanning_playlists")
		_ = os.WriteFile(out, []byte("partial mkv"), 0o644)
		fmt.Println("#GUI#progress 25%")
		if kind == "hang" {
			time.Sleep(time.Minute)
			return 0
		}
		fmt.Println("#GUI#progress 100%")
		code, _ := strconv.Atoi(value)
		switch code {
		case 1:
			fmt.Println("Warning: the playlist has a gap")
			fmt.Println(`#GUI#warning A warning: with C:\Users\bob\stuff`)
		case 2:
			fmt.Println("Error: cannot open the playlist")
			fmt.Println(`#GUI#error The file 'C:\Users\bob\x.ts' could not be opened for reading`)
		}
		return code
	}
	return 3
}

// fake returns an Mkvmerge that runs the test binary as a fake mkvmerge.
func fake(mode string, extraEnv ...string) *Mkvmerge {
	return &Mkvmerge{
		Path:    os.Args[0],
		Version: Version{Major: 102},
		pre:     []string{"-test.run=^TestHelperProcess$", "--"},
		env:     append([]string{"ZENVIK_FAKE_MKVMERGE=" + mode}, extraEnv...),
	}
}
