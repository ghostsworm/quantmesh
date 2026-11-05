#!/usr/bin/env ruby
# frozen_string_literal: true

# Isolated (no frontend artifacts):
# go test ./web/api_backtest_optim.go ./web/replay_optimizer_api_test.go -count=1 -v
# Full package: go test ./web -run '^TestReplayOptimizerAPIWithRuby$' -count=1 -v
# Optional REPLAY_OPTIMIZER_REPORT_DIR writes both JSON and Markdown reports.
require 'json'
require 'net/http'
require 'uri'
require 'time'
require 'fileutils'

class ReplayOptimizerAPITest
  PATH = '/api/backtest/optim/tasks'

  def initialize
    @base = URI(ENV.fetch('REPLAY_OPTIMIZER_API_URL'))
    local_hosts = ['localhost', '::1', [127, 0, 0, 1].join('.')]
    raise 'isolated localhost API harness required' unless @base.scheme == 'http' && local_hosts.include?(@base.host)

    @checks = []
  end

  def request(method, path, body = nil)
    uri = URI.join(@base.to_s, path)
    http = Net::HTTP.new(uri.host, uri.port, nil) # Never use proxy/account environment settings.
    http.open_timeout = 5
    http.read_timeout = 20
    req = Net::HTTP.const_get(method.capitalize).new(uri.request_uri)
    req['Content-Type'] = 'application/json'
    req.body = body.is_a?(String) ? body : JSON.generate(body) unless body.nil?
    response = http.request(req)
    [response.code.to_i, JSON.parse(response.body)]
  end

  def assert(condition, message)
    raise message unless condition
  end

  def check(name)
    yield
    @checks << { name: name, status: 'passed' }
  rescue StandardError => e
    @checks << { name: name, status: 'failed', error: e.message }
    raise
  end

  def payload
    {
      'strategy' => 'grid', 'symbol' => 'BTCUSDT', 'interval' => '1h',
      'start_time' => '2025-01-01T00:00:00Z', 'end_time' => '2025-01-06T00:00:00Z',
      'total_capital' => 10_000,
      'search_space' => {
        'strategy' => 'grid', 'ranges' => { 'grid_spacing' => { 'min' => 20, 'max' => 40, 'step' => 20 } },
        'replay_walk_forward' => {
          'train_days' => 3, 'test_days' => 1, 'step_days' => 1, 'leverage' => 3,
          'base_params' => {
            'grid_spacing' => 20, 'order_quantity' => 100,
            'buy_window_size' => 3, 'sell_window_size' => 3,
            'price_decimals' => 2, 'quantity_decimals' => 4, 'intrabar_steps_per_leg' => 1,
            'maker_fee_rate' => 0.0002, 'taker_fee_rate' => 0.0004,
            'funding_enabled' => true, 'funding_rate' => 0.0001,
            'regime_filter' => { 'enabled' => true, 'kline_interval' => '1h' }
          }
        }
      }
    }
  end

  def state
    code, body = request('get', '/__test/state')
    assert(code == 200, "state HTTP #{code}: #{body}")
    body
  end

  def wait_until
    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + 45
    loop do
      value = yield
      return value if value
      raise 'timed out waiting for API state' if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline
      sleep 0.03
    end
  end

  def reject(name, status = 400)
    check(name) do
      body = payload
      yield body
      before = state
      code, result = request('post', PATH, body)
      assert(code == status && result['success'] == false, "expected HTTP #{status}, got #{code}: #{result}")
      after = state
      assert(after['creates'] == before['creates'] && after['loads'] == before['loads'], 'rejected request reached store or loader')
    end
  end

  def run
    reject('grid requires explicit replay opt-in') { |p| p['search_space'].delete('replay_walk_forward') }
    reject('null replay cannot fall back to legacy') { |p| p['search_space']['replay_walk_forward'] = nil }
    reject('replay is grid-only') { |p| p['strategy'] = p['search_space']['strategy'] = 'momentum' }
    reject('strategy mismatch rejected') { |p| p['search_space']['strategy'] = 'momentum' }
    reject('equal timestamps rejected') { |p| p['end_time'] = p['start_time'] }
    reject('reversed timestamps rejected') { |p| p['end_time'] = '2024-01-01T00:00:00Z' }
    reject('future end rejected') { |p| p['end_time'] = '9999-01-01T00:00:00Z' }
    reject('nonpositive capital rejected') { |p| p['total_capital'] = -1 }
    reject('33 candidates rejected') { |p| p['search_space']['ranges']['grid_spacing'] = { 'min' => 1, 'max' => 33, 'step' => 1 } }
    reject('too many folds rejected') { |p| p['end_time'] = '2025-01-17T00:00:00Z' }
    reject('too many candles rejected') { |p| p['interval'] = '1m'; p['end_time'] = '2025-05-01T00:00:00Z' }
    reject('evaluation budget enforced') do |p|
      p['interval'] = '1m'
      p['end_time'] = '2025-02-13T00:00:00Z'
      p['search_space']['replay_walk_forward'].merge!('train_days' => 30, 'test_days' => 2, 'step_days' => 2)
      p['search_space']['ranges']['grid_spacing'] = { 'min' => 1, 'max' => 32, 'step' => 1 }
    end
    reject('unknown fixed parameter rejected') { |p| p['search_space']['replay_walk_forward']['base_params']['unknown_feature'] = true }
    reject('future funding signal rejected') { |p| p['search_space']['replay_walk_forward']['base_params']['funding_pricing'] = true }
    reject('future funding object rejected') { |p| p['search_space']['replay_walk_forward']['base_params']['funding_rate'] = { 'enabled' => true, 'pricing_enabled' => true } }
    reject('aggtrade path rejected') { |p| p['search_space']['replay_walk_forward']['base_params']['aggtrade_dir'] = '/tmp/unused-replay-trades' }
    reject('unknown candidate axis rejected') { |p| p['search_space']['ranges'] = { 'risk_volume_multiplier' => { 'min' => 1, 'max' => 2, 'step' => 1 } } }
    reject('request size bounded before persistence', 413) { |p| p['padding'] = 'x' * (256 * 1024) }

    id = nil
    completed = nil
    check('replay admitted and all evaluations tracked') do
      code, body = request('post', PATH, payload)
      assert(code == 200 && body['success'], "create HTTP #{code}: #{body}")
      id = body.fetch('task_id')
      completed = wait_until do
        code, response = request('get', "#{PATH}/#{id}")
        assert(code == 200, "task HTTP #{code}: #{response}")
        task = response.fetch('task')
        assert(task['completed_combos'] <= task['total_combos'], "progress exceeds total: #{task}")
        assert(!%w[failed stopped].include?(task['status']), "task ended #{task['status']}: #{task['error']}")
        task if task['status'] == 'completed'
      end
      assert(completed['total_combos'] == 10 && completed['completed_combos'] == 10 && completed['progress'] == 100, "wrong replay progress: #{completed}")
      assert(completed['search_space'] == payload['search_space'], 'persisted replay search space changed')
      assert(state['progress_violations'].zero?, 'worker violated persisted progress bounds')
    end

    result = nil
    check('results contain train selection and held-out baseline comparisons') do
      code, result = request('get', "#{PATH}/#{id}/result")
      assert(code == 200, "result HTTP #{code}: #{result}")
      replay = result.fetch('replay_walk_forward')
      assert(replay['engine'] == 'replay' && replay['folds'].length == 2, 'unexpected engine or fold count')
      assert(result['all_results'] == [] && result['best_by_return'].nil?, 'replay leaked a legacy full-sample ranking')
      assert(replay['baseline_params']['grid_spacing'] == 20, 'fixed baseline was not preserved')
      %w[config_sha256 data_sha256 experiment_sha256 baseline_sha256].each do |key|
        assert(replay[key].match?(/\A[0-9a-f]{64}\z/), "missing experiment fingerprint #{key}")
      end
      replay['folds'].each do |fold|
        assert(fold['train_end'] == fold['test_start'] && fold['test_end'] > fold['test_start'], 'invalid train/test boundary')
        assert(fold['candidates'].length == 2, 'missing candidate training results')
        assert(fold['candidates'].all? { |candidate| candidate.key?('train') && !candidate.key?('test') }, 'candidate selection can see held-out scores')
        %w[train test baseline_train baseline_test].each do |key|
          assert(fold[key].key?('fees_total') && fold[key].key?('funding_paid'), "missing replay costs for #{key}")
        end
      end
    end

    check('persisted configuration and result survive manager reload') do
      code, = request('post', '/__test/reload')
      assert(code == 200, 'manager reload failed')
      code, body = request('get', "#{PATH}/#{id}")
      assert(code == 200 && body['task']['search_space'] == payload['search_space'], 'reloaded protocol differs')
      code, body = request('get', "#{PATH}/#{id}/result")
      assert(code == 200 && body == result, 'reloaded result differs')
    end

    check('legacy nil replay tasks and results remain readable') do
      code, body = request('get', "#{PATH}/legacy")
      assert(code == 200 && body['task']['search_space']['replay_walk_forward'].nil?, 'legacy task unreadable')
      code, body = request('get', "#{PATH}/legacy/result")
      assert(code == 200 && body['task_id'] == 'legacy' && body['replay_walk_forward'].nil?, 'legacy result unreadable')
      code, body = request('get', PATH)
      assert(code == 200 && body['tasks'].any? { |task| task['id'] == id }, 'persisted task missing from list')
    end

    check('capacity rejection and delete cancel the active loader') do
      request('post', '/__test/block')
      code, body = request('post', PATH, payload)
      assert(code == 200, "blocked create HTTP #{code}: #{body}")
      blocked_id = body.fetch('task_id')
      wait_until { state['active_loads'] == 1 }
      before = state
      code, body = request('post', PATH, payload)
      assert(code == 429 && !body['success'], "capacity HTTP #{code}: #{body}")
      assert(state['creates'] == before['creates'], 'capacity-rejected task was persisted')
      code, body = request('delete', "#{PATH}/#{blocked_id}")
      assert(code == 200 && body['success'], "delete HTTP #{code}: #{body}")
      after = state
      assert(after['canceled'] == 1 && after['active_loads'].zero?, 'delete did not wait for canceled loader')
      code, = request('get', "#{PATH}/#{blocked_id}")
      assert(code == 404, 'deleted task still exists')
      code, = request('get', "#{PATH}/#{blocked_id}/result")
      assert(code == 404, 'canceled task produced a successful result')
    end
    true
  rescue StandardError => e
    warn "Replay optimizer HTTP verification failed: #{e.message}"
    false
  ensure
    report
  end

  def report
    failed = @checks.count { |c| c[:status] == 'failed' }
    document = { suite: 'replay_optimizer_api', passed: @checks.length - failed, failed: failed, fixture: 'synthetic_local_file', checks: @checks }
    json = JSON.pretty_generate(document)
    markdown = "# Replay optimizer API 验证\n\n通过 #{@checks.length - failed}，失败 #{failed}。数据为隔离合成样本，不代表实盘收益。\n\n| 检查 | 结果 |\n| --- | --- |\n"
    @checks.each { |c| markdown << "| #{c[:name]} | #{c[:status]}#{c[:error] ? ": #{c[:error].gsub('|', '/')}" : ''} |\n" }
    if ENV['REPLAY_OPTIMIZER_REPORT_DIR']
      dir = File.expand_path(ENV.fetch('REPLAY_OPTIMIZER_REPORT_DIR'))
      FileUtils.mkdir_p(dir)
      File.write(File.join(dir, 'replay_optimizer_api.json'), json + "\n")
      File.write(File.join(dir, 'replay_optimizer_api.md'), markdown)
    end
    puts json
    puts markdown
  end
end

exit(ReplayOptimizerAPITest.new.run ? 0 : 1)
