#!/usr/bin/env bash
# Tests for signing/signpath-artifact-configuration.xml: it parses, signs
# exactly zenvik.exe and zenvik-gui.exe in the release zips' folders, and
# never mkvmerge.exe.
#
#   scripts/signpath-config_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
cfg="$here/../signing/signpath-artifact-configuration.xml"
python3 - "$cfg" <<'PY'
import sys, xml.etree.ElementTree as ET
ns = {"s": "http://signpath.io/artifact-configuration/v1"}
passed = failed = 0
def check(name, ok, detail=""):
    global passed, failed
    if ok:
        passed += 1; print("ok   " + name)
    else:
        failed += 1; print("FAIL " + name + (("\n     " + detail) if detail else ""))
try:
    root = ET.parse(sys.argv[1]).getroot()
except Exception as e:
    check("parses as XML", False, str(e)); print(f"{passed} passed, {failed} failed"); sys.exit(1)
check("parses as XML", True)
check("root is artifact-configuration in SignPath's namespace", root.tag == "{%s}artifact-configuration" % ns["s"], root.tag)
outer = root.findall("s:zip-file", ns)
check("one outer zip-file (the GitHub artifact)", len(outer) == 1 and "path" not in outer[0].attrib)
signed = []
for z in root.iter("{%s}zip-file" % ns["s"]):
    for d in z.findall("s:directory", ns):
        for pe in d.findall("s:pe-file", ns):
            if pe.find("s:authenticode-sign", ns) is not None:
                signed.append((z.get("path"), d.get("path"), pe.get("path")))
want = [("zenvik_*_windows_amd64.zip", "zenvik_*_windows_amd64", "zenvik.exe"),
        ("zenvik-gui_*_windows_amd64.zip", "zenvik-gui_*_windows_amd64", "zenvik-gui.exe")]
check("signs zenvik.exe and zenvik-gui.exe in the release zips' folders", sorted(signed) == sorted(want), repr(signed))
paths = [e.get("path", "") for e in root.iter()]
check("no element targets mkvmerge.exe", not any("mkvmerge" in p.lower() for p in paths), repr(paths))
print(f"{passed} passed, {failed} failed")
sys.exit(0 if failed == 0 else 1)
PY
