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

## Cleaning up after a crash

Press Ctrl-C once to stop a rip cleanly. A second Ctrl-C exits immediately and may leave
the image mounted or a `.partial` file behind. To remove a leftover mount:

- macOS: `hdiutil info`, then `hdiutil detach -force <mount point>` (zenvik mounts under a
  temporary directory named `zenvik-mount-*`).
- Linux: `udisksctl unmount -b /dev/loopN && udisksctl loop-delete -b /dev/loopN`.
- Windows: `Dismount-DiskImage -ImagePath <image>`.
