#!/usr/bin/env bash
# Structural checks of .github/workflows/release.yml: Windows signing, and
# the release- artifact prefix publish relies on. Uses Ruby's built-in YAML.
#
#   scripts/release-workflow_test.sh [path/to/release.yml]
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
wf=${1:-$here/../.github/workflows/release.yml}
ruby -ryaml - "$wf" <<'RUBY'
w = YAML.load_file(ARGV[0])
jobs = w['jobs']
$pass = 0
$fail = 0
def check(name, ok, detail = '')
  if ok
    $pass += 1
    puts "ok   #{name}"
  else
    $fail += 1
    puts "FAIL #{name}"
    puts "     #{detail}" unless detail.to_s.empty?
  end
end
steps = ->(j) { jobs.dig(j, 'steps') || [] }
uses = ->(s, a) { s['uses'].to_s.start_with?(a) }

pub = jobs['publish'] || {}
check 'publish needs windows-sign and windows-sign-check', (Array(pub['needs']) & %w[windows-sign windows-sign-check]).size == 2, pub['needs'].inspect
dl = steps.call('publish').select { |s| uses.call(s, 'actions/download-artifact') }
check 'publish downloads only release-* artifacts', dl.size == 1 && dl[0].dig('with', 'pattern') == 'release-*' && dl[0].dig('with', 'merge-multiple') == true, dl.inspect

build = %w[cli cli-darwin gui-darwin gui-other windows-sign]
names = build.flat_map { |j| steps.call(j).select { |s| uses.call(s, 'actions/upload-artifact') }.map { |s| [j, s.dig('with', 'name').to_s] } }
bad = names.reject { |_, n| n.start_with?('release-', 'windows-unsigned-') || n == 'windows-to-sign' || n == '${{ matrix.artifact }}' }
check 'build jobs upload only release-*, windows-unsigned-* or windows-to-sign', bad.empty?, bad.inspect

inc = jobs.dig('gui-other', 'strategy', 'matrix', 'include') || []
check 'gui-other names its artifacts per matrix entry', inc.map { |e| e['artifact'] }.compact.sort == %w[release-gui-linux windows-unsigned-gui], inc.inspect
cli_up = steps.call('cli').select { |s| uses.call(s, 'actions/upload-artifact') }.map { |s| [s.dig('with', 'name'), s.dig('with', 'path')] }
check 'cli uploads the Windows zip only as windows-unsigned-cli', cli_up.include?(['windows-unsigned-cli', 'dist/*.zip']) && cli_up.include?(['release-cli', 'dist/*.tar.gz']) && cli_up.size == 2, cli_up.inspect

ws = jobs['windows-sign'] || {}
check 'windows-sign runs in the release environment', ws['environment'] == 'release', ws['environment'].inspect
check 'windows-sign needs cli and gui-other', (Array(ws['needs']) & %w[cli gui-other]).size == 2, ws['needs'].inspect
perm = ws['permissions'] || {}
check 'windows-sign grants actions: read and contents: read (SignPath reads job details)', perm['actions'] == 'read' && perm['contents'] == 'read', perm.inspect
ups = steps.call('windows-sign').select { |s| uses.call(s, 'actions/upload-artifact') }
check 'windows-sign uploads overwrite (a re-run reuses the names)', !ups.empty? && ups.all? { |s| s.dig('with', 'overwrite') == true }, ups.map { |s| s.dig('with', 'name') }.inspect
check 'windows-sign outputs signed', ws.dig('outputs', 'signed').to_s.include?('steps.mode.outputs.signed'), ws['outputs'].inspect
sp = steps.call('windows-sign').find { |s| uses.call(s, 'signpath/github-action-submit-signing-request@') }
check 'the SignPath action is pinned to a commit SHA', !sp.nil? && sp['uses'] =~ /@[0-9a-f]{40}\z/, sp.inspect
check 'signing waits up to 4 hours', !sp.nil? && sp.dig('with', 'wait-for-completion-timeout-in-seconds').to_s == '14400'
check 'signing uses project zenvik and policy release-signing', !sp.nil? && sp.dig('with', 'project-slug') == 'zenvik' && sp.dig('with', 'signing-policy-slug') == 'release-signing'
users = jobs.select { |_, j| j.to_s.include?('SIGNPATH_API_TOKEN') }.keys
check 'only windows-sign reads SIGNPATH_API_TOKEN', users == ['windows-sign'], users.inspect

wc = jobs['windows-sign-check'] || {}
check 'windows-sign-check needs windows-sign and always runs', Array(wc['needs']) == ['windows-sign'] && !wc.key?('if'), wc.slice('needs', 'if').inspect
check 'windows-sign-check runs on Windows', wc['runs-on'] == 'windows-latest', wc['runs-on'].inspect

puts "#{$pass} passed, #{$fail} failed"
exit($fail.zero? ? 0 : 1)
RUBY
