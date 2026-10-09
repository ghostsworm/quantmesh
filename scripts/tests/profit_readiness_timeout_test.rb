require 'minitest/autorun'
require 'json'
require 'open3'
require 'tmpdir'
require 'fileutils'
require 'rbconfig'
require 'yaml'

class ProfitReadinessTimeoutTest < Minitest::Test
  SCRIPT = File.expand_path('../verify_profit_readiness.rb', __dir__)

  def test_help_flag_does_not_run_tests_or_overwrite_a_report_named_help
    Dir.mktmpdir('quantmesh-profit-runner-help-') do |directory|
      report = File.join(directory, '--help')
      FileUtils.mkdir_p(report)
      File.write(File.join(report, 'results.json'), 'preserve-json')
      File.write(File.join(report, 'results.md'), 'preserve-markdown')
      marker = File.join(directory, 'go-was-run')
      env = install_go_probe(directory, marker)

      stdout, _stderr, status = Open3.capture3(env, RbConfig.ruby, SCRIPT, '--help', chdir: directory)

      assert status.success?
      assert_includes stdout, 'Usage: ruby scripts/verify_profit_readiness.rb OUTPUT_DIRECTORY [--full]'
      refute File.exist?(marker)
      assert_equal 'preserve-json', File.read(File.join(report, 'results.json'))
      assert_equal 'preserve-markdown', File.read(File.join(report, 'results.md'))
    end
  end

  def test_unknown_option_fails_before_running_tests_or_creating_output
    Dir.mktmpdir('quantmesh-profit-runner-invalid-option-') do |directory|
      marker = File.join(directory, 'go-was-run')
      env = install_go_probe(directory, marker)

      _stdout, stderr, status = Open3.capture3(env, RbConfig.ruby, SCRIPT, '--unknown', chdir: directory)

      assert_equal 2, status.exitstatus
      assert_includes stderr, 'invalid option: --unknown'
      refute File.exist?(marker)
      refute File.exist?(File.join(directory, '--unknown'))
    end
  end

  def run_fixture(mode = nil, action: 'pass', go_exit: 0, cases: 11, malformed: false, secret_diagnostics: false, nested_diagnostics: false, build_diagnostics: false)
    Dir.mktmpdir('quantmesh-profit-runner-contract-') do |directory|
      executable = File.join(directory, 'go')
      File.write(executable, [
        '#!/usr/bin/env ruby',
        "require 'json'",
        "action = ENV.fetch('QUANTMESH_RUNNER_FIXTURE_ACTION')",
        "Integer(ENV.fetch('QUANTMESH_RUNNER_FIXTURE_CASES')).times do |i|",
        "  puts JSON.generate('Package' => 'quantmesh/strategy', 'Test' => 'TestFixture' + i.to_s, 'Action' => action, 'Elapsed' => 0)",
        'end',
        "puts JSON.generate('Package' => 'quantmesh/strategy', 'Action' => 'pass')",
        "puts JSON.generate('Package' => 'quantmesh/strategy', 'Action' => 'output', 'Output' => 'api_key=fixture-api-secret') if ENV.fetch('QUANTMESH_RUNNER_FIXTURE_SECRETS') == '1'",
        "if ENV.fetch('QUANTMESH_RUNNER_FIXTURE_BUILD_DIAGNOSTICS') == '1'",
        "  puts JSON.generate('ImportPath' => 'quantmesh/strategy', 'Action' => 'build-output', 'Output' => 'strategy.go:42: undefined: MissingSymbol APIKey=fixture-build-secret\\n')",
        "  puts JSON.generate('Package' => 'quantmesh/strategy', 'Action' => 'fail', 'FailedBuild' => 'quantmesh/strategy')",
        'end',
        "[['TestFixture0', 'parent diagnostic'], ['TestFixture0/child', 'child diagnostic'], ['TestFixture0/child/deep', 'deep diagnostic'], ['TestFixture01/child', 'neighbor diagnostic']].each { |name, output| puts JSON.generate('Package' => 'quantmesh/strategy', 'Test' => name, 'Action' => 'output', 'Output' => output) } if ENV.fetch('QUANTMESH_RUNNER_FIXTURE_NESTED') == '1'",
        "puts 'not-json token=fixture-parser-secret' if ENV.fetch('QUANTMESH_RUNNER_FIXTURE_MALFORMED') == '1'",
        "warn 'Authorization: Bearer fixture-bearer-secret password=fixture-stderr-secret' if ENV.fetch('QUANTMESH_RUNNER_FIXTURE_SECRETS') == '1'",
        "exit Integer(ENV.fetch('QUANTMESH_RUNNER_FIXTURE_EXIT'))"
      ].join("\n") + "\n")
      FileUtils.chmod(0o755, executable)
      output = File.join(directory, 'report')
      env = {
        'PATH' => directory + File::PATH_SEPARATOR + ENV.fetch('PATH'),
        'QUANTMESH_RUNNER_FIXTURE_ACTION' => action,
        'QUANTMESH_RUNNER_FIXTURE_CASES' => cases.to_s,
        'QUANTMESH_RUNNER_FIXTURE_EXIT' => go_exit.to_s,
        'QUANTMESH_RUNNER_FIXTURE_MALFORMED' => malformed ? '1' : '0',
        'QUANTMESH_RUNNER_FIXTURE_SECRETS' => secret_diagnostics ? '1' : '0',
        'QUANTMESH_RUNNER_FIXTURE_NESTED' => nested_diagnostics ? '1' : '0',
        'QUANTMESH_RUNNER_FIXTURE_BUILD_DIAGNOSTICS' => build_diagnostics ? '1' : '0'
      }
      stdout, stderr, status = Open3.capture3(env, RbConfig.ruby, SCRIPT, output, *[mode].compact)
      assert File.file?(File.join(output, 'results.json')), "report absent: #{stdout} #{stderr}"
      assert File.file?(File.join(output, 'results.md'))
      yield JSON.parse(File.read(File.join(output, 'results.json'))), status, output
    end
  end

  def test_original_audit_keeps_bounded_timeout_and_all_cases
    run_fixture do |report, status|
      assert status.success?
      assert_includes report.fetch('command'), '-timeout=120s'
      assert_includes report.fetch('command'), '^TestAudit'
      assert_equal 11, report.fetch('totals').fetch('pass')
      assert_match(/\A[0-9a-f]{64}\z/, report.fetch('source_tree_sha256'))
      assert_equal !report.fetch('worktree_status').empty?, report.fetch('source_dirty')
      assert_equal report.fetch('server_version'), report.fetch('frontend_version')
      assert report.fetch('versions_consistent')
      assert report.fetch('source_stable_during_run')
    end
  end

  def test_markdown_report_identifies_source_tree_and_versions
    run_fixture do |report, _status, output|
      markdown = File.read(File.join(output, 'results.md'))
      assert_includes markdown, report.fetch('baseline_commit')
      assert_includes markdown, report.fetch('source_tree_sha256')
      assert_includes markdown, report.fetch('server_version')
      assert_includes markdown, report.fetch('frontend_version')
      assert_includes markdown, '测试期间源码稳定：true'
    end
  end

  def test_full_repository_has_go_default_budget_without_exclusions
    run_fixture('--full') do |report, status|
      assert status.success?
      assert_equal ['go', 'test', '-json', './...', '-count=1', '-timeout=10m'], report.fetch('command')
      refute_includes report.fetch('command'), '-run'
      assert_includes report.fetch('scope'), 'NOT complete production or profitability acceptance'
    end
  end

  def test_legacy_full_alias_uses_the_same_complete_command
    run_fixture('--full-with-known-pending') do |report, status|
      assert status.success?
      assert_equal ['go', 'test', '-json', './...', '-count=1', '-timeout=10m'], report.fetch('command')
    end
  end

  def test_reported_failures_are_not_hidden_by_zero_subprocess_exit
    run_fixture('--full', action: 'fail') do |report, status|
      assert_equal 1, status.exitstatus
      assert_equal 11, report.fetch('totals').fetch('fail')
    end
  end

  def test_malformed_output_and_credentials_are_redacted_and_fail_closed
    run_fixture('--full', malformed: true, secret_diagnostics: true) do |report, status, output|
      assert_equal 1, status.exitstatus
      assert_equal 1, report.fetch('parse_errors')
      contents = [File.read(File.join(output, 'results.json')), File.read(File.join(output, 'results.md'))].join("\n")
      %w[fixture-api-secret fixture-parser-secret fixture-bearer-secret fixture-stderr-secret].each do |secret|
        refute_includes contents, secret
      end
      assert_includes contents, '[REDACTED]'
      assert_includes contents, 'not-json'
    end
  end

  def test_nonzero_subprocess_exit_propagates_even_with_pass_events
    run_fixture('--full', go_exit: 1) do |report, status|
      assert_equal 1, status.exitstatus
      assert_equal 1, report.fetch('exit_code')
    end
  end

  def test_go_build_diagnostics_are_retained_and_redacted
    run_fixture('--full', go_exit: 1, build_diagnostics: true) do |report, status, output|
      assert_equal 1, status.exitstatus
      diagnostic = report.fetch('build_diagnostics').find { |event| event.fetch('ImportPath') == 'quantmesh/strategy' }
      refute_nil diagnostic
      assert_includes diagnostic.fetch('Output'), 'strategy.go:42: undefined: MissingSymbol'
      refute_includes diagnostic.fetch('Output'), 'fixture-build-secret'
      assert_includes report.fetch('package_results').map { |event| event['Action'] }, 'fail'
      markdown = File.read(File.join(output, 'results.md'))
      assert_includes markdown, '## Go 包结果'
      assert_includes markdown, '| quantmesh/strategy | fail |'
      assert_includes markdown, 'Go build: quantmesh/strategy'
      assert_includes markdown, 'strategy.go:42: undefined: MissingSymbol'
      refute_includes markdown, 'fixture-build-secret'
    end
  end

  def test_skipped_original_audit_does_not_pass_acceptance
    run_fixture(action: 'skip') do |report, status|
      assert_equal 1, status.exitstatus
      assert_equal 11, report.fetch('totals').fetch('skip')
    end
  end

  def test_missing_test_evidence_cannot_pass_full_gate
    run_fixture('--full', cases: 0) do |report, status|
      assert_equal 1, status.exitstatus
      assert_empty report.fetch('cases')
    end
  end

  def test_test_output_is_indexed_for_parent_and_descendant_without_prefix_collisions
    run_fixture('--full', nested_diagnostics: true) do |report, status|
      assert status.success?
      parent = report.fetch('cases').find { |item| item.fetch('test') == 'TestFixture0' }
      assert_includes parent.fetch('output'), 'parent diagnostic'
      assert_includes parent.fetch('output'), 'child diagnostic'
      assert_includes parent.fetch('output'), 'deep diagnostic'
      refute_includes parent.fetch('output'), 'neighbor diagnostic'
    end
  end

  def test_ci_and_cd_run_the_contract_without_failure_tolerance
    root = File.expand_path('../..', __dir__)
    %w[ci cd].each do |name|
      workflow = YAML.safe_load(File.read(File.join(root, '.github/workflows', name + '.yml')))
      job = workflow.fetch('jobs').fetch('test')
      step = job.fetch('steps').find { |item| item['name'] == 'Verify trading race gate contract' }
      assert step, "#{name}: contract gate missing"
      assert_includes step.fetch('run'), 'ruby scripts/tests/profit_readiness_timeout_test.rb'
      refute step['continue-on-error']
      refute job['continue-on-error']
    end
  end

  private

  def install_go_probe(directory, marker)
    executable = File.join(directory, 'go')
    File.write(executable, "#!#{RbConfig.ruby}\nFile.write(ENV.fetch('QUANTMESH_GO_RUN_MARKER'), 'called')\n")
    FileUtils.chmod(0o755, executable)
    {
      'PATH' => directory + File::PATH_SEPARATOR + ENV.fetch('PATH'),
      'QUANTMESH_GO_RUN_MARKER' => marker
    }
  end
end
