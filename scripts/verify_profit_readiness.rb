#!/usr/bin/env ruby
# Runs the original eleven audit reproductions without hiding pending failures.
require 'json'
require 'open3'
require 'fileutils'
require 'time'

root = File.expand_path('..', __dir__)
output_dir = File.expand_path(ARGV.fetch(0) { abort 'Usage: ruby scripts/verify_profit_readiness.rb OUTPUT_DIRECTORY [--full]' })
full_repository = ['--full', '--full-with-known-pending'].include?(ARGV[1])
command = ['go', 'test', '-json', './position', './risk', './strategy', './web', '-run', '^TestAudit', '-count=1', '-timeout=120s']
if full_repository
  command = ['go', 'test', '-json', './...', '-count=1', '-timeout=120s']
end
stdout, stderr, status = Open3.capture3(*command, chdir: root)
events = stdout.lines.filter_map do |line|
  begin
    JSON.parse(line)
  rescue JSON::ParserError
    nil
  end
end
cases = events.select { |e| e['Test'] && !e['Test'].include?('/') && %w[pass fail skip].include?(e['Action']) }.map do |e|
  outputs = events.select { |x| x['Package'] == e['Package'] && x['Action'] == 'output' && (x['Test'] == e['Test'] || x['Test']&.start_with?(e['Test'] + '/')) }
  { package: e['Package'], test: e['Test'], outcome: e['Action'], elapsed_seconds: e['Elapsed'], output: outputs.map { |x| x['Output'] }.join }
end
sha, = Open3.capture3('git', 'rev-parse', 'HEAD', chdir: root)
worktree, = Open3.capture3('git', 'status', '--short', chdir: root)
report = {
  generated_at: Time.now.iso8601, baseline_commit: sha.strip, worktree_status: worktree,
  command: command, exit_code: status.exitstatus, cases: cases,
  totals: cases.group_by { |c| c[:outcome] }.transform_values(&:length),
  package_results: events.select { |e| !e['Test'] && %w[pass fail skip].include?(e['Action']) },
  package_output: events.select { |e| !e['Test'] && e['Action'] == 'output' },
  stderr: stderr,
  scope: full_repository ? 'Full repository regression with no excluded audit tests. NOT complete production or profitability acceptance.' : 'Original audit regressions only; not a complete production or profitability acceptance gate.'
}
FileUtils.mkdir_p(output_dir)
File.write(File.join(output_dir, 'results.json'), JSON.pretty_generate(report) + "\n")
title = full_repository ? '# 实盘准备度整仓回归（无审查用例排除项）' : '# 实盘准备度原始复现回归'
markdown = [title, '', "时间：#{report[:generated_at]}", '',
            report[:scope], '', '不代替完整重启恢复、真实成交对账和盈利验证；原始复现全部通过不表示审查各组已全部闭合。', '',
            '| 包 | 用例 | 结果 |', '|---|---|---|']
cases.each { |c| markdown << "| #{c[:package]} | #{c[:test]} | #{c[:outcome]} |" }
cases.select { |c| c[:outcome] != 'pass' }.each do |c|
  markdown.concat(['', "## #{c[:test]}", '', '```text', c[:output], '```'])
end
File.write(File.join(output_dir, 'results.md'), markdown.join("\n") + "\n")
puts JSON.generate({ totals: report[:totals], case_count: cases.length, exit_code: status.exitstatus, output_directory: output_dir })
acceptable = full_repository ? cases.none? { |c| c[:outcome] == 'fail' } : cases.all? { |c| c[:outcome] == 'pass' }
exit(status.success? && cases.length >= 11 && acceptable ? 0 : 1)
