#!/usr/bin/env ruby
require 'json'
require 'open3'
require 'fileutils'
require 'time'

module TradingRaceVerification
  ROOT = File.expand_path('..', __dir__)
  PACKAGES = %w[. ./strategy ./monitor ./execution ./order ./position ./risk ./storage ./web ./exchange/binance].freeze
  MYSQL_CASES = %w[
    TestMySQLOpeningPauseOwnersAreIndependentByInstance
    TestMySQLFundingPaymentIdentityAndCoverage
    TestMySQLAccountWalletCapitalReservations
    TestMySQLMarginInterestLedgerAndCoverage
    TestMySQLOrderFillCoverageMigrationAndRoundTrip
    TestMySQLSpotInventorySnapshotMigrationAndRoundTrip
    TestMySQLBackfillTradesBotIDFromOrdersRejectsAmbiguousOwnership
    TestMySQLProfitWithdrawRules
  ].freeze

  def self.command
    ['go', 'test', '-race', '-json', *PACKAGES, '-count=1', '-timeout=180s']
  end

  def self.summarize(stdout, exit_code, module_path, require_mysql: false)
    events = []
    parse_errors = 0
    stdout.each_line do |line|
      begin
        event = JSON.parse(line)
        if event.is_a?(Hash)
          events << event
        else
          parse_errors += 1
        end
      rescue JSON::ParserError
        parse_errors += 1
      end
    end
    terminal = events.select { |event| %w[pass fail skip].include?(event['Action']) }
    cases = terminal.select { |event| event['Test'] && !event['Test'].include?('/') }.map do |event|
      event.slice('Package', 'Test', 'Action', 'Elapsed')
    end
    packages = terminal.reject { |event| event['Test'] }.map { |event| event.slice('Package', 'Action', 'Elapsed') }
    required = PACKAGES.map { |path| path == '.' ? module_path : "#{module_path}/#{path.delete_prefix('./')}" }
    missing = required.reject do |package|
      packages.any? { |event| event['Package'] == package && event['Action'] == 'pass' } &&
        cases.any? { |event| event['Package'] == package && event['Action'] == 'pass' }
    end
    missing_mysql = MYSQL_CASES.reject do |name|
      evidence = cases.select { |event| event['Package'] == "#{module_path}/storage" && event['Test'] == name }
      evidence.length == 1 && evidence.first['Action'] == 'pass'
    end
    success = exit_code == 0 && parse_errors.zero? && missing.empty? && terminal.none? { |event| event['Action'] == 'fail' } &&
      (!require_mysql || missing_mysql.empty?)
    {
      success: success, exit_code: exit_code, parse_errors: parse_errors, missing_verified_packages: missing,
      totals: cases.group_by { |event| event['Action'] }.transform_values(&:length), cases: cases, packages: packages,
      mysql_required: require_mysql, missing_verified_mysql_cases: missing_mysql,
      scope: 'Local trading-core race regression only. Skips are not acceptance; this does not prove production deployment, real-account reconciliation or profitability.'
    }
  end

  def self.run(output_dir, require_mysql: false)
    module_path = File.read(File.join(ROOT, 'go.mod'))[/^module\s+(\S+)/, 1]
    abort 'Missing Go module identity' unless module_path
    sha, _, git_status = Open3.capture3('git', 'rev-parse', 'HEAD', chdir: ROOT)
    worktree, _, worktree_status = Open3.capture3('git', 'status', '--porcelain', chdir: ROOT)
    source_version = JSON.parse(File.read(File.join(ROOT, 'webui/package.json'))).fetch('version')
    stdout, stderr, status = Open3.capture3(*command, chdir: ROOT)
    report = summarize(stdout, status.exitstatus, module_path, require_mysql: require_mysql).merge(
      generated_at: Time.now.iso8601, source_commit: git_status.success? ? sha.strip : nil,
      source_version: source_version, source_dirty: !worktree.empty?, worktree_status: worktree,
      command: command, stderr_present: !stderr.empty?
    )
    report[:success] = false unless git_status.success? && worktree_status.success?
    FileUtils.mkdir_p(output_dir)
    File.write(File.join(output_dir, 'results.json'), JSON.pretty_generate(report) + "\n")
    lines = ['# 交易核心竞争检测', '', "提交：#{report[:source_commit]}", "版本：#{source_version}", "未提交改动：#{report[:source_dirty]}", "时间：#{report[:generated_at]}",
             "结果：#{report[:success] ? '通过' : '失败'}", '', report[:scope], '',
             "命令：`#{command.join(' ')}`", '', '| 包 | 用例 | 结果 |', '|---|---|---|']
    report[:cases].each { |event| lines << "| #{event['Package']} | #{event['Test']} | #{event['Action']} |" }
    lines.concat(['', "未核实包：#{report[:missing_verified_packages].join(', ')}", "JSON 解析错误：#{report[:parse_errors]}",
                  "强制 MySQL 证据：#{require_mysql}", "未核实 MySQL 用例：#{report[:missing_verified_mysql_cases].join(', ')}"])
    File.write(File.join(output_dir, 'results.md'), lines.join("\n") + "\n")
    puts JSON.generate(report.slice(:success, :exit_code, :totals, :missing_verified_packages, :parse_errors, :mysql_required, :missing_verified_mysql_cases).merge(output_directory: output_dir))
    report[:success] ? 0 : 1
  end
end

if $PROGRAM_NAME == __FILE__
  usage = 'Usage: ruby scripts/verify_trading_race.rb OUTPUT_DIRECTORY [--require-mysql]'
  abort usage if ARGV.empty? || ARGV.length > 2 || (ARGV[1] && ARGV[1] != '--require-mysql')
  output_dir = File.expand_path(ARGV[0])
  exit TradingRaceVerification.run(output_dir, require_mysql: ARGV[1] == '--require-mysql')
end
