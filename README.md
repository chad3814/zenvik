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
```

`<path>` is an ISO image, a folder containing `BDMV`, or a `BDMV` folder.

Exit codes: 0 success, 1 failure, 2 usage error, 3 unsupported or encrypted source,
4 missing or too-old dependency.
