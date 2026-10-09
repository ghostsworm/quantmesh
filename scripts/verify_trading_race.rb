#!/usr/bin/env ruby
require 'json'
require 'open3'
require 'fileutils'
require 'time'
require_relative 'source_provenance'

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
    TestMySQLStrategyRuntimeStateConditionalWritePreservesNewEvidence
    TestMySQLFundingCarryRuntimeGenerationFencesOldOwner
    TestMySQLFundingCarryFullConstructorBorrowReceiptRetainsCapitalAndOwnership
    TestMySQLFundingCarryFullConstructorRepaymentReceiptRetainsCapitalAndOwnership
    TestMySQLFundingCarryFullConstructorFinalVerificationReleasesOnlyAfterReadonlyRetry
    TestMySQLFundingCarryFullConstructorCommittedCASFailureRecoversWithoutFinancialReplay
    TestMySQLFundingCarryFullConstructorCleanupFailureRetainsCapital
    TestMySQLFundingCarryFullConstructorStreamCleanupAndFinalVerificationRecoverWithoutFinancialReplay
    TestMySQLFundingCarryFullConstructorCapitalCommittedReplyFailureRecoversWithoutFinancialReplay
    TestMySQLFundingCarryFullConstructorCapitalCommitWireInterruptionRecoversWithoutFinancialReplay
    TestMySQLFundingCarryRuntimeGenerationAdapterCommitOutcomeRecovery
  ].freeze
  MYSQL_CONSTRUCTOR_CASES = %w[
    TestMySQLFundingCarryFullConstructorBorrowReceiptRetainsCapitalAndOwnership
    TestMySQLFundingCarryFullConstructorRepaymentReceiptRetainsCapitalAndOwnership
    TestMySQLFundingCarryFullConstructorFinalVerificationReleasesOnlyAfterReadonlyRetry
    TestMySQLFundingCarryFullConstructorCommittedCASFailureRecoversWithoutFinancialReplay
    TestMySQLFundingCarryFullConstructorCleanupFailureRetainsCapital
    TestMySQLFundingCarryFullConstructorStreamCleanupAndFinalVerificationRecoverWithoutFinancialReplay
    TestMySQLFundingCarryFullConstructorCapitalCommittedReplyFailureRecoversWithoutFinancialReplay
    TestMySQLFundingCarryFullConstructorCapitalCommitWireInterruptionRecoversWithoutFinancialReplay
    TestMySQLFundingCarryRuntimeGenerationAdapterCommitOutcomeRecovery
  ].freeze
  MYSQL_CONSTRUCTOR_MODES = %w[verified_receipt wrong_identity changed_checkpoint].freeze
  MYSQL_FINAL_VERIFICATION_MODES = %w[StopBot StopAll].freeze
  MYSQL_COMMITTED_CAS_MODES = %w[StopBot_ack_error StopBot_cancelled_commit StopAll_ack_error StopAll_cancelled_commit].freeze
  MYSQL_STREAM_CLEANUP_MODES = %w[StopBot_cleanup StopAll_cleanup].freeze
  MYSQL_CAPITAL_COMMIT_MODES = %w[
    StopBot_capital_ack_error StopBot_capital_cancelled_commit StopBot_capital_before_commit_error StopBot_capital_before_commit_cancel
    StopAll_capital_ack_error StopAll_capital_cancelled_commit StopAll_capital_before_commit_error StopAll_capital_before_commit_cancel
  ].freeze
  MYSQL_CAPITAL_WIRE_MODES = %w[StopBot_capital_wire_before_commit StopBot_capital_wire_after_commit StopAll_capital_wire_before_commit StopAll_capital_wire_after_commit].freeze
  MYSQL_RUNTIME_GENERATION_COMMIT_MODES = %w[
    save_before_commit save_after_commit_ack_lost save_owner_takeover save_commit_err_uncommitted
    cas_before_commit cas_after_commit_ack_lost cas_owner_takeover cas_commit_err_uncommitted cas_after_commit_ack_lost_cancelled
  ].freeze

  def self.mysql_constructor_modes(name)
    return MYSQL_RUNTIME_GENERATION_COMMIT_MODES if name == 'TestMySQLFundingCarryRuntimeGenerationAdapterCommitOutcomeRecovery'
    return MYSQL_CAPITAL_WIRE_MODES if name == 'TestMySQLFundingCarryFullConstructorCapitalCommitWireInterruptionRecoversWithoutFinancialReplay'
    return MYSQL_CAPITAL_COMMIT_MODES if name == 'TestMySQLFundingCarryFullConstructorCapitalCommittedReplyFailureRecoversWithoutFinancialReplay'
    return MYSQL_FINAL_VERIFICATION_MODES if name == 'TestMySQLFundingCarryFullConstructorCleanupFailureRetainsCapital'
    return MYSQL_STREAM_CLEANUP_MODES if name == 'TestMySQLFundingCarryFullConstructorStreamCleanupAndFinalVerificationRecoverWithoutFinancialReplay'
    return MYSQL_COMMITTED_CAS_MODES if name == 'TestMySQLFundingCarryFullConstructorCommittedCASFailureRecoversWithoutFinancialReplay'
    return MYSQL_FINAL_VERIFICATION_MODES if name == 'TestMySQLFundingCarryFullConstructorFinalVerificationReleasesOnlyAfterReadonlyRetry'
    MYSQL_CONSTRUCTOR_CASES.include?(name) ? MYSQL_CONSTRUCTOR_MODES : []
  end

  def self.mysql_case_package(name, module_path)
    MYSQL_CONSTRUCTOR_CASES.include?(name) ? module_path : "#{module_path}/storage"
  end

  def self.command
    ['go', 'test', '-race', '-json', *PACKAGES, '-count=1', '-timeout=1200s']
  end

  # Diagnostic text is evidence, never executable instructions. Remove common
  # credential formats before either report is written; do not dump environment.
  def self.redact_diagnostic(value)
    text = value.to_s.dup
    text.gsub!(/-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----/m, '[REDACTED PRIVATE KEY]')
    text.gsub!(/\bBearer\s+[^\s,;"']+/i, 'Bearer [REDACTED]')
    text.gsub!(/((?:api[_-]?key|api[_-]?secret|listen[_-]?key|access[_-]?token|refresh[_-]?token|password|secret|token|webhook(?:[_-]?url)?|authorization|dsn|cookie)\s*["']?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)/i, '\1[REDACTED]')
    text.gsub!(/([a-z][a-z0-9+.-]*:\/\/)[^\/\s@]+@/i, '\1[REDACTED]@')
    text
  end

  def self.failure_diagnostics(events, terminal)
    terminal.select { |event| event['Action'] == 'fail' }.map do |failure|
      output = events.select do |event|
        event['Action'] == 'output' && event['Package'] == failure['Package'] &&
          (!failure['Test'] || event['Test'] == failure['Test'] || event['Test']&.start_with?(failure['Test'] + '/'))
      end.map { |event| event['Output'].to_s }.join
      failure.slice('Package', 'Test', 'Action', 'Elapsed').merge('Output' => redact_diagnostic(output))
    end
  end

  def self.append_diagnostic(lines, title, output)
    return if output.empty?
    fence = '`' * [3, output.scan(/`+/).map(&:length).max.to_i + 1].max
    lines.concat(['', "### #{title}", '', "#{fence}text", output, fence])
  end

  def self.summarize(stdout, exit_code, module_path, require_mysql: false)
    events = []
    parse_errors = 0
    unparsed_output = []
    stdout.each_line do |line|
      begin
        event = JSON.parse(line)
        if event.is_a?(Hash)
          events << event
        else
          parse_errors += 1
          unparsed_output << redact_diagnostic(line)
        end
      rescue JSON::ParserError
        parse_errors += 1
        unparsed_output << redact_diagnostic(line)
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
      evidence = cases.select { |event| event['Package'] == mysql_case_package(name, module_path) && event['Test'] == name }
      parent_verified = evidence.length == 1 && evidence.first['Action'] == 'pass'
      children_verified = mysql_constructor_modes(name).all? do |mode|
        children = terminal.select { |event| event['Package'] == module_path && event['Test'] == "#{name}/#{mode}" }
        children.length == 1 && children.first['Action'] == 'pass'
      end
      parent_verified && children_verified
    end
    success = exit_code == 0 && parse_errors.zero? && missing.empty? && terminal.none? { |event| event['Action'] == 'fail' } &&
      (!require_mysql || missing_mysql.empty?)
    {
      success: success, exit_code: exit_code, parse_errors: parse_errors, missing_verified_packages: missing,
      totals: cases.group_by { |event| event['Action'] }.transform_values(&:length), cases: cases, packages: packages,
      failure_diagnostics: failure_diagnostics(events, terminal), unparsed_output: unparsed_output,
      mysql_required: require_mysql, missing_verified_mysql_cases: missing_mysql,
      mysql_evidence: terminal.select do |event|
        MYSQL_CASES.any? { |name| event['Test'] == name || event['Test']&.start_with?("#{name}/") }
      end.map { |event| event.slice('Package', 'Test', 'Action', 'Elapsed') },
      scope: 'Local trading-core race regression only. Skips are not acceptance; this does not prove production deployment, real-account reconciliation or profitability.'
    }
  end

  def self.run(output_dir, require_mysql: false)
    module_path = File.read(File.join(ROOT, 'go.mod'))[/^module\s+(\S+)/, 1]
    abort 'Missing Go module identity' unless module_path
    source_before = SourceProvenance.capture(ROOT)
    stdout, stderr, status = Open3.capture3(*command, chdir: ROOT)
    source_after = SourceProvenance.capture(ROOT)
    source_stable_during_run = source_before[:source_provenance_complete] && source_after[:source_provenance_complete] &&
      source_before[:baseline_commit] == source_after[:baseline_commit] &&
      source_before[:source_tree_sha256] == source_after[:source_tree_sha256] &&
      source_before[:server_version] == source_after[:server_version] &&
      source_before[:frontend_version] == source_after[:frontend_version]
    versions_consistent = source_before[:versions_consistent] && source_after[:versions_consistent]
    report = summarize(stdout, status.exitstatus, module_path, require_mysql: require_mysql).merge(
      generated_at: Time.now.iso8601, source_commit: source_before[:baseline_commit],
      baseline_commit: source_before[:baseline_commit], source_version: source_before[:frontend_version],
      source_dirty: source_before[:source_dirty], worktree_status: source_before[:worktree_status],
      source_tree_sha256: source_before[:source_tree_sha256], source_tree_sha256_after: source_after[:source_tree_sha256],
      source_stable_during_run: source_stable_during_run,
      server_version: source_before[:server_version], frontend_version: source_before[:frontend_version],
      versions_consistent: versions_consistent,
      command: command, stderr_present: !stderr.empty?, stderr: redact_diagnostic(stderr)
    )
    report[:success] = false unless source_stable_during_run && versions_consistent
    FileUtils.mkdir_p(output_dir)
    File.write(File.join(output_dir, 'results.json'), JSON.pretty_generate(report) + "\n")
    lines = ['# 交易核心竞争检测', '', "提交：#{report[:source_commit]}", "版本：#{report[:source_version]}", "未提交改动：#{report[:source_dirty]}",
             "源码快照 SHA-256：#{report[:source_tree_sha256]}", "测试期间源码稳定：#{report[:source_stable_during_run]}",
             "后端/前端版本：#{report[:server_version] || '未知'} / #{report[:frontend_version]}（一致：#{report[:versions_consistent]}）", "时间：#{report[:generated_at]}",
             "结果：#{report[:success] ? '通过' : '失败'}", '', report[:scope], '',
             "命令：`#{command.join(' ')}`", '', '| 包 | 用例 | 结果 |', '|---|---|---|']
    report[:cases].each { |event| lines << "| #{event['Package']} | #{event['Test']} | #{event['Action']} |" }
    lines.concat(['', "未核实包：#{report[:missing_verified_packages].join(', ')}", "JSON 解析错误：#{report[:parse_errors]}",
                  "强制 MySQL 证据：#{require_mysql}", "未核实 MySQL 用例：#{report[:missing_verified_mysql_cases].join(', ')}"])
    lines.concat(['', '## MySQL 必需父例与子路径证据', '', '| 包 | 用例 | 结果 |', '|---|---|---|'])
    report[:mysql_evidence].each { |event| lines << "| #{event['Package']} | #{event['Test']} | #{event['Action']} |" }
    lines.concat(['', '## 失败诊断证据（常见凭据格式已脱敏）'])
    report[:failure_diagnostics].each do |entry|
      append_diagnostic(lines, [entry['Package'], entry['Test'] || '(package)'].join(' / '), entry['Output'])
    end
    append_diagnostic(lines, 'stderr', report[:stderr])
    append_diagnostic(lines, '未解析输出', report[:unparsed_output].join)
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
