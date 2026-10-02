require 'minitest/autorun'
require 'yaml'
require_relative '../verify_trading_race'

class TradingRaceGateTest < Minitest::Test
  ROOT = File.expand_path('../..', __dir__)

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

  def result(events, exit_code = 0, require_mysql: false)
    TradingRaceVerification.summarize(events.map { |event| JSON.generate(event) }.join("\n"), exit_code, 'quantmesh', require_mysql: require_mysql)
  end

  def mysql_events
    TradingRaceVerification::MYSQL_CASES.map { |name| { 'Package' => 'quantmesh/storage', 'Test' => name, 'Action' => 'pass' } }
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
    assert_equal 8, result(complete_events, require_mysql: true)[:missing_verified_mysql_cases].length
    # Local-only regression may still report a partial acceptance boundary,
    # while both actual release workflows require the stricter mode.
    report = result(complete_events)
    assert report[:success]
    refute report[:mysql_required]
    assert_equal 8, report[:missing_verified_mysql_cases].length
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
