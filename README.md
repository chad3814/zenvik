# zenvik

A friendly command-line tool and Go library for remuxing **unencrypted** Blu-ray disc images
(ISO or BDMV folders) to MKV. zenvik never decrypts anything.

Status: early development. See `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`.

License: MIT.


## Usage

```
zenvik info <path>          # list titles; ★ marks the likely main feature
zenvik info --all <path>    # include filtered titles and why they were filtered
zenvik info --json <path>   # machine-readable output
zenvik rip <path>                     # remux the main feature to ./<disc name>.mkv
zenvik rip -p 00801 -o ~/Movies <path> # a specific playlist, into ~/Movies
zenvik rip --dry-run <path>           # show the output path and mkvmerge command
zenvik rip --name "Big Buck Bunny" --year 2008 <path>   # → ./Big Buck Bunny (2008).mkv
zenvik rip --preset plex <path>                          # use a config preset
zenvik doctor                                            # check mkvmerge, mounting, config, leftovers
```

`<path>` is an ISO image, a folder containing `BDMV`, or a `BDMV` folder.

Exit codes: 0 success, 1 failure, 2 usage error, 3 unsupported or encrypted source,
4 missing or too-old dependency.

## Requirements

- [MKVToolNix](https://mkvtoolnix.download/) 80 or newer (`mkvmerge` on your `PATH`).
- Ripping from an ISO mounts it read-only for the duration of the rip:
  - macOS: `hdiutil` (built in).
  - Linux: `udisksctl` (package `udisks2`). Headless systems may need polkit permission.
  - Windows: PowerShell `Mount-DiskImage` (built in).
  If mounting isn't possible, mount or extract the image yourself and pass the folder.

## Configuration

zenvik reads `$XDG_CONFIG_HOME/zenvik/config.toml` (default `~/.config/zenvik/config.toml`;
`%AppData%\zenvik\config.toml` on Windows). Every key is optional:

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

Precedence: command-line flags, then the selected preset, then top-level values, then the
defaults. Presets may set `output_dir`, `template` and `min_duration`. Unknown keys, bad
durations and unknown presets are errors (exit 2), so typos don't go unnoticed.

### Name templates

| Variable | Value |
|---|---|
| `{name}` | `--name`, else the disc's title, else its volume label tidied up (`THE_MATRIX` → `The Matrix`) |
| `{year}` | `--year` |
| `{label}` | the raw volume label |
| `{playlist}` | the playlist number, e.g. `00800` |

`[...]` is dropped when a variable inside it is empty, so `{name}[ ({year})]` gives `Movie.mkv`
without a year. `/` in the template makes folders. Characters that are invalid on any OS are
replaced with `_`, names reserved on Windows (like `CON`) get a `_` suffix, and `.mkv` is added
if missing.

## Cleaning up after a crash

Run `zenvik doctor` to list mounts a crashed rip left behind, with the command to remove each.
That command also deletes the mount's record file from zenvik's state directory
(`$XDG_STATE_HOME/zenvik/mounts`, else your OS user cache directory).

Press Ctrl-C once to stop a rip cleanly. A second Ctrl-C exits immediately and may leave
the image mounted or a `.partial` file behind. To remove a leftover mount:

- macOS: `hdiutil info`, then `hdiutil detach -force <mount point>` (zenvik mounts under a
  temporary directory named `zenvik-mount-*`).
- Linux: `udisksctl unmount -b /dev/loopN && udisksctl loop-delete -b /dev/loopN`.
- Windows: `Dismount-DiskImage -ImagePath <image>`.
