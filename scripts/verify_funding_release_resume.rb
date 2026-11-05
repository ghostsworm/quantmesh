#!/usr/bin/env ruby
# 以隔離的 Go 裝配執行管理員 API 驗收，輸出可核對的 JSON 與 Markdown。
require 'json'
require 'open3'
require 'optparse'
require 'fileutils'
require 'time'
require_relative 'source_provenance'
require_relative 'verify_trading_race'

# Desktop shells may leave LANG unset, making File.read default to US-ASCII and
# fail on UTF-8 Go sources. Acceptance must not depend on the caller's locale.
Encoding.default_external = Encoding::UTF_8

options = { mysql: false, race: false }
parser = OptionParser.new do |cli|
  cli.banner = 'Usage: ruby scripts/verify_funding_release_resume.rb OUTPUT_DIRECTORY [--mysql] [--race]'
  cli.on('--mysql', 'Require isolated MySQL release-resume cases to pass') { options[:mysql] = true }
  cli.on('--race', 'Enable the Go race detector') { options[:race] = true }
end
begin
  parser.parse!(ARGV)
rescue OptionParser::ParseError => error
  abort "#{error.message}\n#{parser}"
end
abort parser.to_s unless ARGV.length == 1 && !ARGV.first.start_with?('-')
if options[:mysql] && (ENV.fetch('QUANTMESH_MYSQL_TEST_DSN', '').empty? || ENV['QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA'] != '1')
  abort 'MySQL acceptance requires the disposable-schema DSN and explicit schema opt-in.'
end
root = File.expand_path('..', __dir__)
output_dir = File.expand_path(ARGV.first)
pattern = 'ReleaseResume|ReleasePending|ReleaseInterruptedAcrossInstances|ProductionFundingRecoveryRouteTakeoverAndFoundUsesRetainedRuntime'
command = ['go', 'test', '-json', '-p', '1']
command << '-race' if options[:race]
command.concat(['./accounting', './storage', './strategy', './web', './internal/app', '-run', pattern, '-count=1', '-timeout=5m'])
before = SourceProvenance.capture(root)
stdout, stderr, status = Open3.capture3(*command, chdir: root)
after = SourceProvenance.capture(root)
events = []
parse_errors = []
stdout.each_line do |line|
  begin
    event = JSON.parse(line)
    raise JSON::ParserError, 'event is not an object' unless event.is_a?(Hash)
    event['Output'] = TradingRaceVerification.redact_diagnostic(event['Output']) if event['Output']
    events << event
  rescue JSON::ParserError
    parse_errors << TradingRaceVerification.redact_diagnostic(line)
  end
end
cases = events.select { |event| event['Test'] && %w[pass fail skip].include?(event['Action']) }
required = %w[
  TestSQLiteFundingRecoveryReleaseResumeAtomicAndReplay
  TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries
  TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries
  TestSQLiteFundingRecoveryReleaseResumeConcurrentPools
  TestSQLiteFundingRecoveryReleaseResumeLateCASRollsBack
  TestSQLiteFundingRecoveryReleaseResumeReopenPreservesOwnerHistory
  TestSQLiteFundingRecoveryReleaseResumeHistoricalReceiptTampering
  TestProductionFundingRecoveryReleaseInterruptedAcrossInstances
  TestProductionFundingRecoveryRouteTakeoverAndFoundUsesRetainedRuntime
  TestFundingCarryFullConstructorRetainsReleasePendingAfterRestart
  TestFundingRecoveryReleaseResumeRequiresSessionAdmin
  TestFundingRecoveryReleaseResumeBindsAuthorizationAndReturnsBothReceipts
  TestFundingRecoveryReleaseResumePreservesDurableErrorState
]
missing = required.reject { |name| cases.any? { |event| event['Test'] == name && event['Action'] == 'pass' } }
mysql_cases = cases.select { |event| event['Test'].match?(/MySQL.*ReleaseResume/) && !event['Test'].include?('/') }
mysql_verified = !mysql_cases.empty? && mysql_cases.all? { |event| event['Action'] == 'pass' }
stable = before[:source_provenance_complete] && after[:source_provenance_complete] &&
  before[:baseline_commit] == after[:baseline_commit] && before[:source_tree_sha256] == after[:source_tree_sha256]
passed = status.success? && parse_errors.empty? && missing.empty? && stable && before[:versions_consistent] &&
  after[:versions_consistent] && (!options[:mysql] || mysql_verified)
report = {
  generated_at: Time.now.utc.iso8601, passed: passed,
  source: before, source_after: after, source_stable_during_run: stable,
  command: command, exit_code: status.exitstatus, parse_errors: parse_errors,
  missing_required_cases: missing, mysql_requested: options[:mysql], mysql_verified: mysql_verified,
  totals: cases.group_by { |event| event['Action'] }.transform_values(&:length),
  cases: cases, output: events.select { |event| %w[output build-output].include?(event['Action']) },
  stderr: TradingRaceVerification.redact_diagnostic(stderr),
  scope: 'Isolated SQLite/MySQL, production router and retained runtime. No production account, deployment, live exchange, or profitability acceptance.'
}
FileUtils.mkdir_p(output_dir)
File.write(File.join(output_dir, 'results.json'), JSON.pretty_generate(report) + "\n")
lines = ['# FundingCarry 管理员解锁续办验收', '',
         "结果：#{passed ? '通过' : '未通过'}", "时间：#{report[:generated_at]}",
         "基线：#{before[:baseline_commit]}", "版本：#{before[:server_version]} / #{before[:frontend_version]}",
         "源码摘要：#{before[:source_tree_sha256]}", "测试期间源码稳定：#{stable}",
         "MySQL 专项完成：#{mysql_verified}", '', report[:scope], '',
         '## 用例', '', '| 包 | 用例 | 结果 |', '|---|---|---|']
cases.each { |event| lines << "| #{event['Package']} | #{event['Test']} | #{event['Action']} |" }
lines.concat(['', "缺失的必选用例：#{missing.join(', ')}", '', '## 执行诊断', '', '```text',
              report[:stderr], *parse_errors, '```'])
cases.select { |event| %w[fail skip].include?(event['Action']) }.each do |test|
  output = report[:output].select { |event| event['Package'] == test['Package'] && event['Test'] == test['Test'] }.map { |event| event['Output'] }.join
  lines.concat(['', "### #{test['Test']}", '', '```text', output, '```'])
end
File.write(File.join(output_dir, 'results.md'), lines.join("\n") + "\n")
puts JSON.generate({ passed: passed, totals: report[:totals], mysql_verified: mysql_verified, source_stable_during_run: stable, output_directory: output_dir })
exit(passed ? 0 : 1)
