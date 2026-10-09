require 'minitest/autorun'
require 'yaml'
require 'tmpdir'
require_relative '../verify_trading_race'

class TradingRaceGateTest < Minitest::Test
  ROOT = File.expand_path('../..', __dir__)

  def test_actual_go_subprocess_failure_retains_redacted_child_assertion
    fixture = File.join(__dir__, 'testdata/race_diagnostics/failure_test.go')
    stdout, _stderr, status = Open3.capture3('go', 'test', '-race', '-json', fixture, '-count=1', '-timeout=30s', chdir: ROOT)
    assert_equal 1, status.exitstatus, 'fixture must actually fail, not become passing evidence'
    report = TradingRaceVerification.summarize(stdout, status.exitstatus, 'quantmesh')
    refute report[:success]
    parent = report.fetch(:failure_diagnostics).find { |entry| entry['Test'] == 'TestDiagnosticFailure' }
    refute_nil parent, 'compile failures cannot stand in for executed fixture assertions'
    assert_match(/failure_test\.go:\d+:/, parent.fetch('Output'))
    assert_includes parent.fetch('Output'), 'diagnostic fixture assertion'
    refute_includes parent.fetch('Output'), 'sample-diagnostic-value'
    assert report[:failure_diagnostics].any? { |entry| entry['Test'] == 'TestDiagnosticFailure/child' }
  end

  def test_common_credential_formats_are_redacted_without_losing_assertion_location
    samples = [
      'fixture.go:42: {"api_key":"sample-api-value"}',
      'fixture.go:42: listenKey=sample-listen-value',
      'fixture.go:42: Authorization: Bearer sample-bearer-value',
      'fixture.go:42: webhook_url=https://example.invalid/sample-hook-value',
      'fixture.go:42: DSN=root:sample-db-value@tcp(127.0.0.1:3306)/fixture',
      'fixture.go:42: https://sample-user-value:sample-password-value@example.invalid/path',
      "fixture.go:42: -----BEGIN PRIVATE KEY-----\nsample-private-value\n-----END PRIVATE KEY-----"
    ]
    samples.each do |sample|
      sanitized = TradingRaceVerification.redact_diagnostic(sample)
      assert_includes sanitized, 'fixture.go:42'
      assert_includes sanitized, '[REDACTED'
      refute_match(/sample-[a-z-]+-value/, sanitized)
    end
  end

  def test_failure_diagnostics_keep_child_assertions_and_package_panics_without_changing_gate
    events = complete_events + [
      { 'Package' => 'quantmesh/web', 'Test' => 'TestMetrics/empty', 'Action' => 'output', 'Output' => "metrics_test.go:57: expected nonnegative CPU\n" },
      { 'Package' => 'quantmesh/web', 'Test' => 'TestOther', 'Action' => 'output', 'Output' => "unrelated passing output\n" },
      { 'Package' => 'quantmesh/web', 'Test' => 'TestMetrics', 'Action' => 'fail' },
      { 'Package' => 'quantmesh/web', 'Action' => 'output', 'Output' => "panic: package worker\n" },
      { 'Package' => 'quantmesh/web', 'Action' => 'fail' }
    ]
    report = result(events, 1)
    refute report[:success]
    test_proof = report.fetch(:failure_diagnostics).find { |entry| entry['Test'] == 'TestMetrics' }
    assert_includes test_proof.fetch('Output'), 'metrics_test.go:57'
    refute_includes test_proof.fetch('Output'), 'unrelated passing output'
    package_proof = report[:failure_diagnostics].find { |entry| !entry['Test'] }
    assert_includes package_proof.fetch('Output'), 'panic: package worker'
    assert_includes package_proof.fetch('Output'), 'metrics_test.go:57'
  end

  def test_real_report_writer_retains_redacted_diagnostics_and_stderr_in_json_and_markdown
    events = complete_events + [
      { 'Package' => 'quantmesh', 'Test' => 'TestBroken', 'Action' => 'output',
        'Output' => "broken_test.go:42: APIKey=fixture-secret Authorization: Bearer fixture-bearer\n```\n" },
      { 'Package' => 'quantmesh', 'Test' => 'TestBroken', 'Action' => 'fail' }
    ]
    status = Struct.new(:exitstatus) { def success? = exitstatus.zero? }.new(1)
    Dir.mktmpdir('quantmesh-race-diagnostics-') do |dir|
      report_status = run_report_with_stub(dir, events, status)
      assert_equal 1, report_status
      report = JSON.parse(File.read(File.join(dir, 'results.json')))
      assert_equal 1, report['parse_errors']
      refute report['success']
      assert_match(/\A[0-9a-f]{64}\z/, report['source_tree_sha256'])
      assert_equal report['source_tree_sha256'], report['source_tree_sha256_after']
      assert report['source_stable_during_run']
      json = File.read(File.join(dir, 'results.json'))
      markdown = File.read(File.join(dir, 'results.md'))
      assert_includes markdown, '````text', 'diagnostic backticks must not terminate the evidence block'
      [json, markdown].each do |content|
        assert_includes content, 'broken_test.go:42'
        assert_includes content, 'compiler failure'
        assert_includes content, 'not-json'
        %w[fixture-secret fixture-bearer fixture-password fixture-parser-secret].each { |secret| refute_includes content, secret }
      end
    end
  end

  def test_report_fails_if_source_changes_during_race_gate
    events = complete_events
    ok = Struct.new(:exitstatus) { def success? = exitstatus.zero? }.new(0)
    diffs = ["stable-before\n", "changed-during-test\n"]
    diff_reads = 0
    capture = lambda do |*args, **_options|
      if args.take(3) == ['git', 'rev-parse', 'HEAD']
        ["fixture-sha\n", '', ok]
      elsif args.take(2) == ['git', 'status']
        [" M tracked.go\n", '', ok]
      elsif args.take(2) == ['git', 'diff']
        value = diffs.fetch([diff_reads, diffs.length - 1].min)
        diff_reads += 1
        [value, '', ok]
      elsif args.take(2) == ['git', 'ls-files']
        ['', '', ok]
      else
        [events.map { |event| JSON.generate(event) }.join("\n") + "\n", '', ok]
      end
    end
    Dir.mktmpdir('quantmesh-race-source-drift-') do |dir|
      exit_code = nil
      Open3.stub(:capture3, capture) do
        capture_io { exit_code = TradingRaceVerification.run(dir) }
      end
      report = JSON.parse(File.read(File.join(dir, 'results.json')))
      assert_equal 1, exit_code
      refute report['success']
      refute report['source_stable_during_run']
      refute_equal report['source_tree_sha256'], report['source_tree_sha256_after']
    end
  end

  def test_ci_and_cd_use_the_same_production_race_gate
    %w[ci cd].each do |name|
      workflow = YAML.safe_load(File.read(File.join(ROOT, '.github/workflows', "#{name}.yml")))
      step = workflow.fetch('jobs').fetch('test').fetch('steps').find { |item| item['name'] == 'Race-test trading risk core' }
      assert step, "#{name}: missing trading race gate"
      assert_equal 'ruby scripts/verify_trading_race.rb /tmp/quantmesh-trading-race --require-mysql', step.fetch('run'), name
      refute step['continue-on-error'], "#{name}: race failures must block the job"
      refute workflow.fetch('jobs').fetch('test')['continue-on-error'], "#{name}: failed test jobs must not be tolerated"
      assert_equal 'test', workflow.fetch('jobs').fetch('build').fetch('needs'), name
      assert_nil workflow.fetch('jobs').fetch('build')['if'], "#{name}: build must retain its default successful dependency condition"
    end
  end

  def test_all_production_race_packages_and_uncached_execution_are_required
    assert_equal %w[. ./strategy ./monitor ./execution ./order ./position ./risk ./storage ./web ./exchange/binance], TradingRaceVerification::PACKAGES
    assert_includes TradingRaceVerification.command, '-race'
    assert_includes TradingRaceVerification.command, '-count=1'
    assert_includes TradingRaceVerification.command, '-timeout=1200s'
    refute_includes TradingRaceVerification.command, '-run'
  end

  def test_release_gate_explicitly_requires_mysql_test_evidence
    %w[ci cd].each do |name|
      workflow = YAML.safe_load(File.read(File.join(ROOT, '.github/workflows', "#{name}.yml")))
      step = workflow.fetch('jobs').fetch('test').fetch('steps').find { |item| item['name'] == 'Race-test trading risk core' }
      assert_includes step.fetch('run'), '--require-mysql', name
    end
  end

  def test_ci_and_cd_use_a_disposable_mysql_service_instead_of_silent_skips
    %w[ci cd].each do |name|
      workflow = YAML.safe_load(File.read(File.join(ROOT, '.github/workflows', "#{name}.yml")))
      job = workflow.fetch('jobs').fetch('test')
      service = job.fetch('services').fetch('mysql')
      assert_equal 'mysql:8.0.36', service.fetch('image')
      assert_equal 'quantmesh_test', service.fetch('env').fetch('MYSQL_DATABASE')
      refute service.key?('volumes'), 'never mount existing database storage'
      assert_equal 'root@tcp(127.0.0.1:3306)/quantmesh_test?parseTime=true', job.fetch('env').fetch('QUANTMESH_MYSQL_TEST_DSN')
      assert_equal '1', job.fetch('env').fetch('QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA')
    end
  end

  def complete_events
    TradingRaceVerification::PACKAGES.flat_map do |path|
      package = path == '.' ? 'quantmesh' : "quantmesh/#{path.delete_prefix('./')}"
      [{ 'Package' => package, 'Test' => 'TestFixture', 'Action' => 'pass' }, { 'Package' => package, 'Action' => 'pass' }]
    end
  end

  def run_report_with_stub(directory, events, go_status)
    ok = Struct.new(:exitstatus) { def success? = exitstatus.zero? }.new(0)
    capture = lambda do |*args, **_options|
      if args.take(3) == ['git', 'rev-parse', 'HEAD']
        ["fixture-sha\n", '', ok]
      elsif args.take(2) == ['git', 'status']
        [" M tracked.go\n", '', ok]
      elsif args.take(2) == ['git', 'diff']
        ["fixture-source-diff\n", '', ok]
      elsif args.take(2) == ['git', 'ls-files']
        ['', '', ok]
      else
        [events.map { |event| JSON.generate(event) }.join("\n") + "\nnot-json api_secret=fixture-parser-secret\n",
         'compiler failure password=fixture-password', go_status]
      end
    end
    exit_code = nil
    Open3.stub(:capture3, capture) do
      capture_io { exit_code = TradingRaceVerification.run(directory) }
    end
    exit_code
  end

  def result(events, exit_code = 0, require_mysql: false)
    TradingRaceVerification.summarize(events.map { |event| JSON.generate(event) }.join("\n"), exit_code, 'quantmesh', require_mysql: require_mysql)
  end

  def mysql_events
    TradingRaceVerification::MYSQL_CASES.flat_map do |name|
      parent = { 'Package' => TradingRaceVerification.mysql_case_package(name, 'quantmesh'), 'Test' => name, 'Action' => 'pass' }
      children = if name.include?('StreamCleanupAndFinalVerification')
                   %w[StopBot_cleanup StopAll_cleanup].map { |mode| parent.merge('Test' => "#{name}/#{mode}") }
                 elsif name.include?('CapitalCommitWireInterruption')
                   %w[StopBot_capital_wire_before_commit StopBot_capital_wire_after_commit StopAll_capital_wire_before_commit StopAll_capital_wire_after_commit].map { |mode| parent.merge('Test' => "#{name}/#{mode}") }
                 elsif name == 'TestMySQLFundingCarryRuntimeGenerationAdapterCommitOutcomeRecovery'
                   TradingRaceVerification.mysql_constructor_modes(name).map { |mode| parent.merge('Test' => "#{name}/#{mode}") }
                 elsif name.include?('CapitalCommittedReplyFailure')
                   %w[StopBot_capital_ack_error StopBot_capital_cancelled_commit StopBot_capital_before_commit_error StopBot_capital_before_commit_cancel StopAll_capital_ack_error StopAll_capital_cancelled_commit StopAll_capital_before_commit_error StopAll_capital_before_commit_cancel].map { |mode| parent.merge('Test' => "#{name}/#{mode}") }
                 elsif name.include?('CleanupFailureRetainsCapital')
                   %w[StopBot StopAll].map { |mode| parent.merge('Test' => "#{name}/#{mode}") }
                 elsif name.include?('CommittedCAS')
                   %w[StopBot_ack_error StopBot_cancelled_commit StopAll_ack_error StopAll_cancelled_commit].map { |mode| parent.merge('Test' => "#{name}/#{mode}") }
                 elsif name.include?('FinalVerification')
                   %w[StopBot StopAll].map { |mode| parent.merge('Test' => "#{name}/#{mode}") }
                 elsif name.include?('FullConstructor')
                   %w[verified_receipt wrong_identity changed_checkpoint].map { |mode| parent.merge('Test' => "#{name}/#{mode}") }
                 else
                   []
                 end
      [parent, *children]
    end
  end

  def test_mysql_requirement_rejects_missing_skipped_failed_wrong_package_and_duplicate_evidence
    events = complete_events + mysql_events
    assert result(events, require_mysql: true)[:success]
    refute result(complete_events, require_mysql: true)[:success]
    %w[skip fail].each do |action|
      bad = mysql_events.map(&:dup)
      bad.first['Action'] = action
      refute result(complete_events + bad, require_mysql: true)[:success]
    end
    wrong_package = mysql_events.map(&:dup)
    wrong_package.first['Package'] = 'quantmesh/order'
    refute result(complete_events + wrong_package, require_mysql: true)[:success]
    refute result(events + [mysql_events.first], require_mysql: true)[:success]
    assert_equal 19, result(complete_events, require_mysql: true)[:missing_verified_mysql_cases].length
    # Local-only regression may still report a partial acceptance boundary,
    # while both actual release workflows require the stricter mode.
    report = result(complete_events)
    assert report[:success]
    refute report[:mysql_required]
    assert_equal 19, report[:missing_verified_mysql_cases].length
  end

  def test_mysql_runtime_generation_fencing_is_required_release_evidence
    name = 'TestMySQLFundingCarryRuntimeGenerationFencesOldOwner'
    assert_includes TradingRaceVerification::MYSQL_CASES, name
    assert_equal 'quantmesh/storage', TradingRaceVerification.mysql_case_package(name, 'quantmesh')
    event = { 'Package' => 'quantmesh/storage', 'Test' => name, 'Action' => 'pass' }
    assert result(complete_events + mysql_events, require_mysql: true)[:success]
    %w[skip fail].each do |action|
      bad = mysql_events.map(&:dup)
      bad.find { |item| item['Test'] == name }['Action'] = action
      refute result(complete_events + bad, require_mysql: true)[:success], action
    end
    refute result(complete_events + mysql_events.reject { |item| item['Test'] == name }, require_mysql: true)[:success], 'missing evidence'
    event['Package'] = 'quantmesh/order'
    bad_package = mysql_events.reject { |item| item['Test'] == name } + [event]
    refute result(complete_events + bad_package, require_mysql: true)[:success], 'wrong package'
  end

  def test_new_mysql_constructor_and_checkpoint_evidence_cannot_be_skipped_or_misattributed
    required = %w[
      TestMySQLStrategyRuntimeStateConditionalWritePreservesNewEvidence
      TestMySQLFundingCarryFullConstructorBorrowReceiptRetainsCapitalAndOwnership
      TestMySQLFundingCarryFullConstructorRepaymentReceiptRetainsCapitalAndOwnership
      TestMySQLFundingCarryFullConstructorFinalVerificationReleasesOnlyAfterReadonlyRetry
      TestMySQLFundingCarryFullConstructorCommittedCASFailureRecoversWithoutFinancialReplay
      TestMySQLFundingCarryFullConstructorCleanupFailureRetainsCapital
      TestMySQLFundingCarryFullConstructorStreamCleanupAndFinalVerificationRecoverWithoutFinancialReplay
    ]
    required.each do |name|
      assert_includes TradingRaceVerification::MYSQL_CASES, name
      expected_package = name.include?('FullConstructor') ? 'quantmesh' : 'quantmesh/storage'
      assert_equal expected_package, TradingRaceVerification.mysql_case_package(name, 'quantmesh')
      %w[skip fail].each do |action|
        events = mysql_events.map(&:dup)
        events.find { |event| event['Test'] == name }['Action'] = action
        refute result(complete_events + events, require_mysql: true)[:success], name
      end
      missing = mysql_events.reject { |event| event['Test'] == name }
      refute result(complete_events + missing, require_mysql: true)[:success], name
      wrong = mysql_events.map(&:dup)
      wrong.find { |event| event['Test'] == name }['Package'] = 'quantmesh/order'
      refute result(complete_events + wrong, require_mysql: true)[:success], name
      event = mysql_events.find { |item| item['Test'] == name }
      refute result(complete_events + mysql_events + [event], require_mysql: true)[:success], name
    end
  end

  def test_constructor_parent_pass_cannot_hide_skipped_missing_duplicate_or_wrong_package_children
    %w[Borrow Repayment].each do |kind|
      parent = "TestMySQLFundingCarryFullConstructor#{kind}ReceiptRetainsCapitalAndOwnership"
      %w[verified_receipt wrong_identity changed_checkpoint].each do |mode|
        name = "#{parent}/#{mode}"
        %w[skip fail].each do |action|
          events = mysql_events.map(&:dup)
          events.find { |event| event['Test'] == name }['Action'] = action
          refute result(complete_events + events, require_mysql: true)[:success], name
        end
        missing = mysql_events.reject { |event| event['Test'] == name }
        refute result(complete_events + missing, require_mysql: true)[:success], name
        wrong = mysql_events.map(&:dup)
        wrong.find { |event| event['Test'] == name }['Package'] = 'quantmesh/storage'
        refute result(complete_events + wrong, require_mysql: true)[:success], name
        event = mysql_events.find { |item| item['Test'] == name }
        refute result(complete_events + mysql_events + [event], require_mysql: true)[:success], name
      end
    end
  end

  def test_final_verification_mysql_requires_both_actual_stop_paths
    parent = 'TestMySQLFundingCarryFullConstructorFinalVerificationReleasesOnlyAfterReadonlyRetry'
    assert_includes TradingRaceVerification::MYSQL_CASES, parent
    assert_equal 'quantmesh', TradingRaceVerification.mysql_case_package(parent, 'quantmesh')
    proof = result(complete_events + mysql_events, require_mysql: true)[:mysql_evidence]
    assert_equal 56, proof.length
    assert_equal %w[StopBot StopAll], proof.select { |event| event['Test'].start_with?("#{parent}/") }.map { |event| event['Test'].delete_prefix("#{parent}/") }
    %w[StopBot StopAll].each do |mode|
      name = "#{parent}/#{mode}"
      %w[skip fail].each do |action|
        events = mysql_events.map(&:dup)
        events.find { |event| event['Test'] == name }['Action'] = action
        refute result(complete_events + events, require_mysql: true)[:success], name
      end
      missing = mysql_events.reject { |event| event['Test'] == name }
      refute result(complete_events + missing, require_mysql: true)[:success], name
      wrong = mysql_events.map(&:dup)
      wrong.find { |event| event['Test'] == name }['Package'] = 'quantmesh/storage'
      refute result(complete_events + wrong, require_mysql: true)[:success], name
      event = mysql_events.find { |item| item['Test'] == name }
      refute result(complete_events + mysql_events + [event], require_mysql: true)[:success], name
    end
  end

  def test_committed_cas_mysql_requires_all_four_actual_stop_fault_paths
    parent = 'TestMySQLFundingCarryFullConstructorCommittedCASFailureRecoversWithoutFinancialReplay'
    assert_includes TradingRaceVerification::MYSQL_CASES, parent
    assert_equal 'quantmesh', TradingRaceVerification.mysql_case_package(parent, 'quantmesh')
    modes = %w[StopBot_ack_error StopBot_cancelled_commit StopAll_ack_error StopAll_cancelled_commit]
    assert_equal modes, TradingRaceVerification.mysql_constructor_modes(parent)
    proof = result(complete_events + mysql_events, require_mysql: true)[:mysql_evidence]
    assert_equal modes, proof.select { |event| event['Test'].start_with?("#{parent}/") }.map { |event| event['Test'].delete_prefix("#{parent}/") }
    modes.each do |mode|
      name = "#{parent}/#{mode}"
      %w[skip fail].each do |action|
        events = mysql_events.map(&:dup)
        events.find { |event| event['Test'] == name }['Action'] = action
        refute result(complete_events + events, require_mysql: true)[:success], name
      end
      missing = mysql_events.reject { |event| event['Test'] == name }
      refute result(complete_events + missing, require_mysql: true)[:success], name
      wrong = mysql_events.map(&:dup)
      wrong.find { |event| event['Test'] == name }['Package'] = 'quantmesh/storage'
      refute result(complete_events + wrong, require_mysql: true)[:success], name
      event = mysql_events.find { |item| item['Test'] == name }
      refute result(complete_events + mysql_events + [event], require_mysql: true)[:success], name
    end
  end

  def test_stream_cleanup_mysql_requires_both_pure_and_mixed_stop_paths
    {
      'TestMySQLFundingCarryFullConstructorCleanupFailureRetainsCapital' => %w[StopBot StopAll],
      'TestMySQLFundingCarryFullConstructorStreamCleanupAndFinalVerificationRecoverWithoutFinancialReplay' => %w[StopBot_cleanup StopAll_cleanup],
      'TestMySQLFundingCarryFullConstructorCapitalCommittedReplyFailureRecoversWithoutFinancialReplay' => %w[StopBot_capital_ack_error StopBot_capital_cancelled_commit StopBot_capital_before_commit_error StopBot_capital_before_commit_cancel StopAll_capital_ack_error StopAll_capital_cancelled_commit StopAll_capital_before_commit_error StopAll_capital_before_commit_cancel],
      'TestMySQLFundingCarryFullConstructorCapitalCommitWireInterruptionRecoversWithoutFinancialReplay' => %w[StopBot_capital_wire_before_commit StopBot_capital_wire_after_commit StopAll_capital_wire_before_commit StopAll_capital_wire_after_commit],
      'TestMySQLFundingCarryRuntimeGenerationAdapterCommitOutcomeRecovery' => %w[save_before_commit save_after_commit_ack_lost save_owner_takeover save_commit_err_uncommitted cas_before_commit cas_after_commit_ack_lost cas_owner_takeover cas_commit_err_uncommitted cas_after_commit_ack_lost_cancelled]
    }.each do |parent, modes|
      assert_includes TradingRaceVerification::MYSQL_CASES, parent
      assert_equal 'quantmesh', TradingRaceVerification.mysql_case_package(parent, 'quantmesh')
      assert_equal modes, TradingRaceVerification.mysql_constructor_modes(parent)
      proof = result(complete_events + mysql_events, require_mysql: true)[:mysql_evidence]
      assert_equal modes, proof.select { |event| event['Test'].start_with?("#{parent}/") }.map { |event| event['Test'].delete_prefix("#{parent}/") }
      modes.each do |mode|
        name = "#{parent}/#{mode}"
        %w[skip fail].each do |action|
          events = mysql_events.map(&:dup)
          events.find { |event| event['Test'] == name }['Action'] = action
          refute result(complete_events + events, require_mysql: true)[:success], name
        end
        missing = mysql_events.reject { |event| event['Test'] == name }
        refute result(complete_events + missing, require_mysql: true)[:success], name
        wrong = mysql_events.map(&:dup)
        wrong.find { |event| event['Test'] == name }['Package'] = 'quantmesh/storage'
        refute result(complete_events + wrong, require_mysql: true)[:success], name
        event = mysql_events.find { |item| item['Test'] == name }
        refute result(complete_events + mysql_events + [event], require_mysql: true)[:success], name
      end
    end
  end

  def test_complete_package_evidence_passes_but_does_not_claim_profitability
    report = result(complete_events)
    assert report[:success]
    assert_includes report[:scope], 'does not prove'
  end

  def test_missing_or_skipped_binance_adapter_evidence_blocks_release
    package = 'quantmesh/exchange/binance'
    missing = complete_events.reject { |event| event['Package'] == package }
    refute result(missing)[:success], 'a release must test the Binance adapter too'
    skipped = complete_events.map(&:dup)
    skipped.each { |event| event['Action'] = 'skip' if event['Package'] == package }
    refute result(skipped)[:success], 'a skipped adapter must not count as verified'
  end

  def test_missing_root_package_zero_cases_failure_and_invalid_json_fail_closed
    refute result(complete_events.reject { |event| event['Package'] == 'quantmesh' })[:success]
    refute result(complete_events.reject { |event| event['Test'] })[:success]
    refute result(complete_events, 1)[:success]
    refute result(complete_events + [{ 'Package' => 'quantmesh', 'Test' => 'TestBroken', 'Action' => 'fail' }])[:success]
    refute TradingRaceVerification.summarize('not json', 0, 'quantmesh')[:success]
    refute TradingRaceVerification.summarize('null', 0, 'quantmesh')[:success]
  end

  def test_skips_are_reported_and_skipped_package_is_not_verified
    events = complete_events
    events.first['Action'] = 'skip'
    report = result(events)
    assert_equal 1, report[:totals]['skip']
    refute report[:success], 'a passed package with all cases skipped is not verified'
    events << { 'Package' => 'quantmesh', 'Test' => 'TestSecond', 'Action' => 'pass' }
    assert result(events)[:success], 'individual reported skips must not hide a real passing case'
    events[1]['Action'] = 'skip'
    refute result(events)[:success]
  end
end
