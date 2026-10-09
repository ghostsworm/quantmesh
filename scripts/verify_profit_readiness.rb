#!/usr/bin/env ruby
# Runs the original eleven audit reproductions without hiding pending failures.
require 'json'
require 'optparse'
require 'open3'
require 'fileutils'
require 'time'
require_relative 'verify_trading_race'
require_relative 'source_provenance'

root = File.expand_path('..', __dir__)
full_repository = false
parser = OptionParser.new do |options|
  options.banner = 'Usage: ruby scripts/verify_profit_readiness.rb OUTPUT_DIRECTORY [--full]'
  options.on('--full', 'Run the complete Go repository test suite') { full_repository = true }
  options.on('--full-with-known-pending', 'Deprecated alias for --full') { full_repository = true }
  options.on('-h', '--help', 'Print this help without running tests') do
    puts options
    exit 0
  end
end

begin
  parser.parse!(ARGV)
rescue OptionParser::ParseError => e
  warn "#{e.message}\n#{parser}"
  exit 2
end

unless ARGV.length == 1
  warn parser
  exit 2
end

output_argument = ARGV.fetch(0)
if output_argument.start_with?('-')
  abort "OUTPUT_DIRECTORY must not start with '-'; prefix a relative path with './'"
end
output_dir = File.expand_path(output_argument)


command = ['go', 'test', '-json', './position', './risk', './strategy', './web', '-run', '^TestAudit', '-count=1', '-timeout=120s']
if full_repository
  # Full suites include recovery/timeout scenarios and routinely exceed 120s.
  # Keep Go's normal 10-minute budget; never omit cases to fit the audit budget.
  command = ['go', 'test', '-json', './...', '-count=1', '-timeout=10m']
end
source_before = SourceProvenance.capture(root)
stdout, stderr, status = Open3.capture3(*command, chdir: root)
source_after = SourceProvenance.capture(root)
source_stable_during_run = source_before[:source_provenance_complete] && source_after[:source_provenance_complete] &&
  source_before[:baseline_commit] == source_after[:baseline_commit] &&
  source_before[:source_tree_sha256] == source_after[:source_tree_sha256] &&
  source_before[:server_version] == source_after[:server_version] &&
  source_before[:frontend_version] == source_after[:frontend_version]
events = []
parse_errors = 0
unparsed_output = []
stdout.each_line do |line|
  begin
    event = JSON.parse(line)
    if event.is_a?(Hash)
      event['Output'] = TradingRaceVerification.redact_diagnostic(event['Output']) if event['Output']
      events << event
    else
      parse_errors += 1
      unparsed_output << TradingRaceVerification.redact_diagnostic(line)
    end
  rescue JSON::ParserError
    parse_errors += 1
    unparsed_output << TradingRaceVerification.redact_diagnostic(line)
  end
end
test_outputs = Hash.new { |packages, package| packages[package] = Hash.new { |tests, test| tests[test] = [] } }
events.each do |event|
  next unless event['Action'] == 'output' && event['Test'] && event['Output']

  test_name = ''
  event['Test'].split('/').each do |segment|
    test_name = test_name.empty? ? segment : "#{test_name}/#{segment}"
    test_outputs[event['Package']][test_name] << event['Output']
  end
end
cases = events.select { |e| e['Test'] && !e['Test'].include?('/') && %w[pass fail skip].include?(e['Action']) }.map do |e|
  output = test_outputs.dig(e['Package'], e['Test']) || []
  { package: e['Package'], test: e['Test'], outcome: e['Action'], elapsed_seconds: e['Elapsed'], output: output.join }
end
report = {
  generated_at: Time.now.iso8601, baseline_commit: source_before[:baseline_commit], worktree_status: source_before[:worktree_status],
  source_dirty: source_before[:source_dirty], source_tree_sha256: source_before[:source_tree_sha256],
  source_tree_sha256_after: source_after[:source_tree_sha256], source_stable_during_run: source_stable_during_run,
  server_version: source_before[:server_version], frontend_version: source_before[:frontend_version],
  versions_consistent: source_before[:versions_consistent] && source_after[:versions_consistent],
  command: command, exit_code: status.exitstatus, parse_errors: parse_errors,
  unparsed_output: unparsed_output, cases: cases,
  totals: cases.group_by { |c| c[:outcome] }.transform_values(&:length),
  package_results: events.select { |e| !e['Test'] && %w[pass fail skip].include?(e['Action']) },
  package_output: events.select { |e| !e['Test'] && e['Action'] == 'output' },
  build_diagnostics: events.select { |e| e['Action'] == 'build-output' }.map do |event|
    event.slice('ImportPath', 'Package', 'Action').merge('Output' => TradingRaceVerification.redact_diagnostic(event['Output']))
  end,
  stderr: TradingRaceVerification.redact_diagnostic(stderr),
  scope: full_repository ? 'Full repository regression with no excluded audit tests. NOT complete production or profitability acceptance.' : 'Original audit regressions only; not a complete production or profitability acceptance gate.'
}
FileUtils.mkdir_p(output_dir)
File.write(File.join(output_dir, 'results.json'), JSON.pretty_generate(report) + "\n")
title = full_repository ? '# 实盘准备度整仓回归（无审查用例排除项）' : '# 实盘准备度原始复现回归'
markdown = [title, '', "时间：#{report[:generated_at]}", "基线提交：#{report[:baseline_commit]}",
            "源码工作树：#{report[:source_dirty] ? '有未提交改动' : '干净'}",
            "源码快照 SHA-256：#{report[:source_tree_sha256]}",
            "后端/前端版本：#{report[:server_version] || '未知'} / #{report[:frontend_version]}（一致：#{report[:versions_consistent]}）",
            "测试期间源码稳定：#{report[:source_stable_during_run]}",
            "Go JSON 解析错误：#{report[:parse_errors]}", '',
            report[:scope], '', '不代替完整重启恢复、真实成交对账和盈利验证；原始复现全部通过不表示审查各组已全部闭合。', '',
            '| 包 | 用例 | 结果 |', '|---|---|---|']
cases.each { |c| markdown << "| #{c[:package]} | #{c[:test]} | #{c[:outcome]} |" }
cases.select { |c| c[:outcome] != 'pass' }.each do |c|
  markdown.concat(['', "## #{c[:test]}", '', '```text', c[:output], '```'])
end
unless report[:package_results].empty?
  markdown.concat(['', '## Go 包结果', '', '| 包 | 结果 | 耗时（秒） |', '|---|---|---:|'])
  report[:package_results].each do |result|
    markdown << "| #{result['Package']} | #{result['Action']} | #{result['Elapsed'] || ''} |"
  end
end
report[:build_diagnostics].each do |diagnostic|
  label = diagnostic['ImportPath'] || diagnostic['Package'] || 'unknown package'
  TradingRaceVerification.append_diagnostic(markdown, "Go build: #{label}", diagnostic['Output'].to_s)
end
unless report[:unparsed_output].empty?
  markdown.concat(['', '## 未解析 Go 输出', '', '```text', report[:unparsed_output].join, '```'])
end
unless report[:stderr].empty?
  markdown.concat(['', '## stderr', '', '```text', report[:stderr], '```'])
end
File.write(File.join(output_dir, 'results.md'), markdown.join("\n") + "\n")
puts JSON.generate({ totals: report[:totals], case_count: cases.length, parse_errors: parse_errors, exit_code: status.exitstatus, output_directory: output_dir })
acceptable = full_repository ? cases.none? { |c| c[:outcome] == 'fail' } : cases.all? { |c| c[:outcome] == 'pass' }
exit(status.success? && parse_errors.zero? && cases.length >= 11 && acceptable && source_stable_during_run && report[:versions_consistent] ? 0 : 1)
