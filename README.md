# zenvik

A friendly command-line tool and Go library for remuxing **unencrypted** Blu-ray and DVD
images and folders to MKV. zenvik never decrypts anything.

Status: early development. See `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`.

License: MIT.

## Install

Download the archive for your platform from the
[releases page](https://github.com/chad3814/zenvik/releases): macOS (Apple silicon `arm64`
or Intel `amd64`), Linux `amd64` or Windows `amd64`. Each archive holds the `zenvik`
binary, this README and the license, and `SHA256SUMS` lists every archive's checksum
(`shasum -a 256 -c SHA256SUMS`). Put `zenvik` somewhere on your `PATH`. zenvik also needs
MKVToolNix (see [Requirements](#requirements)). `zenvik --version` prints the release.

The macOS binaries are not signed or notarized, so Gatekeeper blocks the first run. Allow
it under System Settings → Privacy & Security, or clear the quarantine flag:

```
xattr -d com.apple.quarantine zenvik
```

To build from source instead: `go install github.com/chad3814/zenvik/cmd/zenvik@latest`
(Go 1.27).

## Usage

```
zenvik info <path>          # list titles; ★ marks the likely main feature
zenvik info --all <path>    # include filtered titles and why they were filtered
zenvik info --json <path>   # machine-readable output
zenvik rip <path>                     # remux the main feature to ./<disc name>.mkv
zenvik rip -p 00801 -d ~/Movies <path> # a specific playlist, into ~/Movies
zenvik rip -o "/srv/rips/My Film.mkv" <path>  # this exact path (no template)
zenvik rip --title 3 <path>           # a specific title (DVD title number or Blu-ray playlist)
zenvik rip --dry-run <path>           # show the output path and mkvmerge command
zenvik rip --jsonl <path>             # machine-readable events (see below)
zenvik rip --name "Big Buck Bunny" --year 2008 <path>   # → ./Big Buck Bunny (2008).mkv
zenvik rip --preset plex <path>                          # use a config preset
zenvik doctor                                            # check mkvmerge, mounting, config, leftovers
```

`rip` names the file from the template (`--template`, else the config's) inside the output
directory (`--output-dir`/`-d`, else the config's `output_dir`, else the current directory).
`--output-file`/`-o` gives the path instead and uses it as is: no template, no name cleaning
and no `.mkv` added. A path starting with `/` (on Windows: a drive letter, `\` or `/`) is
absolute; any other path is inside the output directory and may leave it with `..`. A leading
`~` is your home directory. `--output-file` can't be combined with `--template`, `--name` or
`--year`.

### Driving `rip` from another program

`zenvik rip --jsonl …` writes [JSON Lines](https://jsonlines.org/) to stdout, one event per line,
and nothing to stderr. The exit code is the same as without `--jsonl`. Paths in events are
absolute. Every event has an `"event"` field:

| event | when | fields |
|---|---|---|
| `start` | the title and output path are resolved | `version` (schema version, 1), `source`, `kind`, `format`, `title`, `duration_seconds`, `size_bytes`, `output`, `auto` (main feature picked automatically), `ambiguous`, `reasons` |
| `progress` | a phase starts, at each whole percent, and at its end | `phase` (`mounting`, `extracting subtitles`, `scanning`, `muxing`, `finalizing`), `fraction` (0–1), `bytes_done`, `bytes_total` |
| `warning` | anything `rip` would print as a warning | `message` |
| `done` | the MKV is written | `output`, `duration_seconds` |
| `dry_run` | instead of `done` with `--dry-run` | `output`, `command` (the mkvmerge program and arguments, as an array) |
| `error` | last line of any failure, including bad flags | `message`, `exit_code`, `canceled` (`true` after SIGINT/SIGTERM) |

```
{"event":"start","version":1,"source":"/discs/MOVIE","kind":"bdmv","format":"bluray","title":"00800",…}
{"event":"progress","phase":"scanning","fraction":0.5,"bytes_done":5670912,"bytes_total":11341824}
{"event":"done","output":"/rips/movie.mkv","duration_seconds":6}
```

To stop a rip, send SIGINT or SIGTERM. zenvik removes the partial file and unmounts anything it
mounted, then ends with an `error` event that has `"canceled": true`.

`<path>` is a Blu-ray or DVD ISO image, a folder containing `BDMV` or `VIDEO_TS`, a `BDMV`
folder, or a `VIDEO_TS` folder. `--title` (`-t`) and `--playlist` (`-p`) are the same option;
passing both is a usage error.

`info --json` prints `format` (`"bluray"` or `"dvd"`) and `kind` (`"iso"`, `"bdmv"` or
`"video_ts"`) for the disc. For each title, `unsupported` (why zenvik can't rip it) appears
when set. Video tracks add `aspect_ratio` (`"4:3"` or `"16:9"`), and audio and subtitle tracks
add `description` (for example `Director's Commentary` or `Forced`), when present. Only
`unsupported`, `aspect_ratio` and `description` are DVD only.

Exit codes: 0 success, 1 failure, 2 usage error, 3 unsupported or encrypted source,
4 missing or too-old dependency.

## DVDs

zenvik reads unencrypted DVD-Video discs: an ISO image, a `VIDEO_TS` folder, or a folder
that contains one. `zenvik info` lists the disc's titles (numbered `01`, `02`, …) and picks
the main feature the same way it does for Blu-ray, including dropping decoy titles whose
data rate is a sliver of the disc's highest. Rip a specific title with `--title`:

```
zenvik rip --title 3 "/path/to/MY_DVD"
```

zenvik passes the title's VOB files to mkvmerge, so it can rip a title only when the title
is made of **whole VOB files** (and only one program chain). That covers most movie main
features and "play all" titles. Titles that start or end in the middle of a VOB file, which
includes most individual TV episodes, can't be ripped yet: they are filtered out of the
default `info` listing, `info --all` shows them with the reason (for example `starts or ends
mid-file (not supported yet)`), and `rip` refuses them with exit code 1. A title is also
unsupported if a VOB's size isn't a multiple of 2048 bytes. CSS-encrypted titles are
detected and refused, never decrypted. `rip` of an encrypted title exits 3, and `info` or
`rip` exits 3 when the only titles zenvik could otherwise use are encrypted.

Each audio and subtitle track keeps its language from the disc. Audio tracks are named with
the codec and channel layout, plus the disc's description when it has one, for example
`AC-3 5.1` or `AC-3 Stereo (Director's Commentary)`. Subtitle tracks are named only when the
disc describes them (`Forced`, `Large`, `Commentary`, …). The first video and audio tracks
are the defaults; no subtitle track is. Chapters come from the disc's chapter table and are
named `Chapter 01`, `Chapter 02`, ….

mkvmerge doesn't read DVD subtitles out of VOB files, so zenvik extracts them to a temporary
VobSub file (with the disc's palette) during the rip and muxes that file as a second input.
Ripping a DVD title with subtitles therefore reads it once before muxing; progress shows this
as `extracting subtitles`. With `--dry-run`, nothing is extracted or written: the command
shows `<subtitles.idx>` and `<chapters.txt>` placeholders. Dry-running an ISO still mounts it,
as for Blu-ray.

## Requirements

- [MKVToolNix](https://mkvtoolnix.download/) 80 or newer (`mkvmerge` on your `PATH`). That is
  all you need to use zenvik. Running the integration tests (`go test -tags integration ./...`)
  also needs `mkvextract`, `ffmpeg` and `dvdauthor` (with `spumux`).
- Ripping from an ISO mounts it read-only for the duration of the rip:
  - macOS: `hdiutil` (built in).
  - Linux: `udisksctl` (package `udisks2`). Headless systems may need polkit permission.
  - Windows: PowerShell `Mount-DiskImage` (built in).
  If mounting isn't possible, mount or extract the image yourself and pass the folder.

## Configuration

zenvik reads `$XDG_CONFIG_HOME/zenvik/config.toml` (default `~/.config/zenvik/config.toml`;
`%AppData%\zenvik\config.toml` on Windows). If the file doesn't exist, `zenvik doctor`
creates it with every key set to its default and a commented-out example preset; it never
changes an existing file. Every key is optional:

```toml
output_dir    = "~/Movies"               # default: current directory
template      = "{name}[ ({year})].mkv"  # default shown
min_duration  = "2m"                     # shorter titles are never the main feature
mkvmerge_path = ""                       # default: search PATH
preset        = ""                       # preset applied when --preset isn't given

[presets.plex]
output_dir = "/Volumes/Media/Movies"
template   = "{name}[ ({year})]/{name}[ ({year})].mkv"
```

A relative `XDG_CONFIG_HOME` is ignored, as the XDG spec requires.

On Windows, write paths as TOML single-quoted literal strings, where a backslash is just a
backslash:

```toml
output_dir    = 'D:\Rips'
mkvmerge_path = 'C:\Program Files\MKVToolNix\mkvmerge.exe'
```

In double quotes a backslash starts an escape, so `"D:\Rips"` would contain a carriage return
(`\r`), and `\t` would become a tab.

Precedence: command-line flags, then the selected preset, then top-level values, then the
defaults. Presets may set `output_dir`, `template` and `min_duration`. Unknown keys, bad
durations, unknown presets and invalid templates are errors (exit 2), so typos don't go
unnoticed. Both `rip` and `info` read the config: `info` also validates the template and the
default preset, and exits 2 if either is invalid.

### Name templates

| Variable | Value |
|---|---|
| `{name}` | `--name`, else the disc's title, else its volume label tidied up (`THE_MATRIX` → `The Matrix`), else `untitled` |
| `{year}` | `--year` |
| `{label}` | the raw volume label |
| `{playlist}` | the playlist number (e.g. `00800`) or DVD title number (e.g. `01`) |

`[...]` is dropped when a variable inside it is empty, so `{name}[ ({year})]` gives `Movie.mkv`
without a year. Outside `[...]` an empty `{year}` is simply left out, and it's an error if that
leaves a folder or file name empty (for example `{year}/{name}` or `[{year}].mkv` without
`--year`).

Only `/` in the template makes folders; a `\` becomes `_`, as does a `/` inside a variable's
value. Characters that are invalid on any OS are replaced with `_`, names reserved on Windows
(like `CON` or `COM1`) get a `_` suffix, and `.mkv` is added if missing.

## Cleaning up after a crash

Run `zenvik doctor` to list mounts a crashed rip left behind, with the command to remove each.
That command also deletes the mount's record file from zenvik's state directory
(`$XDG_STATE_HOME/zenvik/mounts`, else your OS user cache directory). On Windows the printed
command is a PowerShell command; elsewhere it is a POSIX shell command. `doctor` itself never
deletes anything; the only file it writes is the default config file, when there is none.

If a record's mount is already gone (after a reboot, or after you detached it by hand), doctor
prints it as a `!` warning, `stale mount record`, with a command that only deletes the record.

`doctor` exits with 4 if mkvmerge is missing or too old, else 2 if the config is invalid, else
1 if there are live leftover mounts, else 0. Stale records and unavailable ISO mounting are
warnings and don't change the exit code.

Press Ctrl-C once to stop a rip cleanly. A second Ctrl-C exits immediately and may leave
the image mounted or a `.partial` file behind. To remove a leftover mount:

- macOS: `hdiutil info`, then `hdiutil detach -force <mount point>` (zenvik mounts under a
  temporary directory named `zenvik-mount-*`).
- Linux: `udisksctl unmount -b /dev/loopN && udisksctl loop-delete -b /dev/loopN`.
- Windows: `Dismount-DiskImage -ImagePath <image>`.

Then run `zenvik doctor`, which shows how to remove the mount's record.
