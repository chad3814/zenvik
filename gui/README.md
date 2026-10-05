# Zenvik (desktop app)

Zenvik is the desktop front end for [zenvik](https://github.com/chad3814/zenvik): add unencrypted Blu-ray and DVD images (`.iso`) or folders (`BDMV`, `VIDEO_TS`, or a folder containing one), tick the titles you want, name each file, and queue rips. Rips run one at a time with mkvmerge and the queue is kept between launches.

The macOS app needs macOS 13 or later. The macOS and Windows apps include `mkvmerge` from [MKVToolNix](https://mkvtoolnix.download) (GPLv2; see `MKVTOOLNIX-NOTICE.txt` in the download, or in `Zenvik.app/Contents/Resources` on macOS, for the license and source). On Linux, install MKVToolNix (mkvmerge) and WebKitGTK 4.1 (`libwebkit2gtk-4.1-0` on Debian/Ubuntu). Setting `mkvmerge_path` in the config makes the app use that mkvmerge instead of the bundled one.

Zenvik reads the same config file as the `zenvik` command (`zenvik doctor` shows where it is): `output_dir`, `template`, `preset`, `min_duration` and `mkvmerge_path` apply here too.

Release builds check GitHub for a newer release at most once a day when the app opens and show a banner with a Download button; **About → Check for updates** checks on demand. `update_check = false` in the config or the `ZENVIK_NO_UPDATE_CHECK` environment variable turns the launch check off.

The macOS app is signed with a Developer ID and notarized by Apple (macOS 13 or later).
