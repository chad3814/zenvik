#!/usr/bin/env bash
# Decide whether a release's Windows zips are signed through SignPath (the
# release workflow's windows-sign job runs this):
#
#   scripts/windows-sign-mode.sh <tag> <signpath-organization-id>
#
# prints "sign" for a final tag (vX.Y.Z) when the organization ID is set;
# otherwise "pass" (publish unsigned), with the reason on stderr.
set -euo pipefail

if [[ $# -ne 2 || -z $1 ]]; then
	echo "usage: $0 <tag> <signpath-organization-id>" >&2
	exit 2
fi
if [[ ! $1 =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "windows-sign: $1 is a pre-release (not vX.Y.Z); publishing the Windows zips unsigned" >&2
	echo pass
	exit 0
fi
if [[ -z $2 ]]; then
	echo "windows-sign: SIGNPATH_ORGANIZATION_ID is not set; publishing the Windows zips unsigned" >&2
	echo pass
	exit 0
fi
echo sign
