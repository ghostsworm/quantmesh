# frozen_string_literal: true

# Invoked by TestMCPTokenClientFlow against a local-only, in-memory fixture.
# No endpoint, token, response body, or exception message goes into reports.
require 'json'
require 'minitest'
require 'minitest/test'
require 'net/http'
require 'uri'

unless ARGV.length == 3
  abort 'Usage: ruby mcp_token_flow_test.rb LOOPBACK_URL REPORT_DIRECTORY SCENARIO'
end

BASE_URL, REPORT_DIRECTORY, SCENARIO = ARGV.freeze
SCENARIOS = %w[no_legacy legacy_write legacy_read].freeze
LEGACY_TOKEN = 'mcp-flow-legacy-fixture'
READ_TOOL = 'fixture_read'
WRITE_A = 'fixture_write_a'
WRITE_B = 'fixture_write_b'
FAILED_WRITE = 'fixture_write_failed'
AUTH_STYLES = %i[url bearer].freeze

begin
  base = URI.parse(BASE_URL)
  valid_base = base.is_a?(URI::HTTP) && base.scheme == 'http' && base.host == [127, 0, 0, 1].join('.') &&
               base.userinfo.nil? && base.query.nil? && base.fragment.nil? && base.path.empty?
rescue URI::InvalidURIError
  valid_base = false
end
abort 'Expected an isolated local fixture and an existing absolute report directory' unless
  valid_base && SCENARIOS.include?(SCENARIO) && REPORT_DIRECTORY.start_with?('/') && File.directory?(REPORT_DIRECTORY)

class MCPTokenFlowTest < Minitest::Test
  attr_reader :stage, :check

  def setup
    @stage = 'setup'
    @check = 'fixture_ready'
    @base = URI.parse(BASE_URL)
  end

  def test_client_flow
    @stage = 'legacy_before_issue'
    verify_legacy
    baseline = state

    @stage = 'issue_readonly'
    reader = issue('HTTP observer', 'readonly')
    AUTH_STYLES.each do |auth|
      verify_read_client(reader, auth)
      denied = call(reader, WRITE_A, auth)
      expect(denied.dig('result', 'isError') == true, 'readonly_write_denied')
      expect(denied.dig('result', 'content', 0, 'text') == '此令牌是只读的，不能执行写操作', 'readonly_denial_reason')
    end
    after_read = state
    expect(after_read['reads'] == baseline['reads'] + AUTH_STYLES.length, 'readonly_handler_executed')
    expect(after_read['writes'] == baseline['writes'], 'readonly_cannot_reach_write_handler')
    expect(after_read['callbacks'] == baseline['callbacks'], 'readonly_no_observer_callback')
    expect(after_read['notices'] == baseline['notices'], 'readonly_no_notification')

    @stage = 'issue_readwrite_and_batch'
    writer = issue('HTTP operator', 'readwrite', notify: { channel: 'email', target: 'fixture@example.invalid' })
    expect(writer['token'] != reader['token'], 'independent_secrets')
    verify_read_client(writer, :bearer)
    listed_writes = tools_for(writer, :bearer)
    [WRITE_A, WRITE_B, FAILED_WRITE].each do |write_tool|
      expect(listed_writes.any? { |name| name == write_tool }, "list_has_write_tool_#{write_tool}")
    end
    bearer_base = state
    bearer_write = call(writer, WRITE_A, :bearer)
    verify_success(bearer_write)
    notify_from_bearer = wait_for_snapshot(bearer_base, 'writes' => 1, 'callbacks' => 1, 'notices_length' => 1)
    expect(notify_from_bearer['writes'] - bearer_base['writes'] == 1, 'bearer_write_executed')
    expect(notify_from_bearer['notices'].last['has_write_a'] == true, 'bearer_write_notified')
    before_batch = notify_from_bearer
    batch = [rpc('tools/call', { name: WRITE_A, arguments: {} }, id: 11),
             rpc('tools/call', { name: READ_TOOL, arguments: {} }, id: 12),
             rpc('tools/call', { name: FAILED_WRITE, arguments: {} }, id: 13),
             rpc('tools/call', { name: WRITE_B, arguments: {} }, id: 14)]
    responses = mcp_request(writer, :url, batch)
    expect(responses.is_a?(Array) && responses.length == batch.length, 'batch_response_count')
    by_id = responses.to_h { |response| [response['id'], response] }
    [11, 12, 14].each { |id| verify_success(by_id.fetch(id)) }
    expect(by_id.dig(13, 'result', 'isError') == true, 'failed_write_reported')
    expect(by_id.dig(13, 'result', 'content', 0, 'text') == 'fixture write rejected', 'failed_write_reason')
    after_batch = wait_for_snapshot(before_batch, 'writes' => 2, 'reads' => 1, 'failed_writes' => 1, 'callbacks' => 1, 'notices_length' => 1)
    expect(after_batch['writes'] == before_batch['writes'] + 2, 'batch_two_successful_writes')
    expect(after_batch['reads'] == before_batch['reads'] + 1, 'batch_read_executed')
    expect(after_batch['failed_writes'] == before_batch['failed_writes'] + 1, 'batch_failed_handler_executed')
    expect(after_batch['callbacks'] == before_batch['callbacks'] + 1, 'batch_one_observer_callback')
    expect(after_batch['notices'].length == before_batch['notices'].length + 1, 'batch_one_notification')
    notice = after_batch['notices'].last
    expect(notice['route_ok'] == true, 'notification_uses_token_destination')
    expect(notice['has_write_a'] && notice['has_write_b'], 'notification_contains_both_successful_writes')
    expect(!notice['has_read'] && !notice['has_failed'], 'notification_excludes_reads_and_failures')
    expect(notice['secret_safe'] == true, 'notification_has_no_secret')

    @stage = 'failed_write_no_notification'
    failed = call(writer, FAILED_WRITE, :bearer)
    expect(failed.dig('result', 'isError') == true, 'standalone_write_failure')
    expect(failed.dig('result', 'content', 0, 'text') == 'fixture write rejected', 'standalone_failure_reason')
    after_failure = wait_for_snapshot(after_batch, 'failed_writes' => 1)
    expect(after_failure['writes'] == after_batch['writes'], 'failed_write_has_no_success')
    expect(after_failure['callbacks'] == after_batch['callbacks'], 'failed_write_has_no_callback')
    expect(after_failure['notices'] == after_batch['notices'], 'failed_write_has_no_notification')

    @stage = 'revoke_readwrite'
    revoke(writer)
    AUTH_STYLES.each do |auth|
      mcp_request(writer, auth, rpc('tools/call', { name: WRITE_A, arguments: {} }), expected: 401)
    end
    after_revoke = state
    expect(after_revoke['writes'] == after_failure['writes'], 'revoked_writer_cannot_execute')
    expect(after_revoke['notices'] == after_failure['notices'], 'revoked_writer_has_no_notification')
    verify_read_client(reader, :url)

    @stage = 'revoke_readonly'
    revoke(reader)
    AUTH_STYLES.each { |auth| mcp_request(reader, auth, rpc('initialize'), expected: 401) }

    @stage = 'legacy_after_revoke'
    verify_legacy
    config = request(:get, '/api/mcp/config')
    expect(config['tokens'] == [], 'revoked_tokens_absent_from_config')
    expect(config['has_token'] == (SCENARIO != 'no_legacy'), 'legacy_presence_preserved')
    final_state = state
    # One notification from the Bearer write plus one from the batch; failed
    # writes and read-only/legacy turns intentionally produce none.
    expect(final_state['notices'].length == baseline['notices'].length + 2, 'only_expected_notifications_sent')
    expect(final_state['notices'].all? { |item| item['secret_safe'] }, 'all_notifications_safe')
    @stage = 'complete'
  end

  private

  def expect(condition, check)
    @check = check
    assert condition, check
  end

  def rpc(method, params = {}, id: 1)
    { jsonrpc: '2.0', id: id, method: method, params: params }
  end

  def issue(name, mode, notify: nil)
    payload = { name: name, mode: mode }
    payload[:notify] = notify if notify
    token = request(:post, '/api/mcp/tokens', payload: payload)
    expect(token['ok'] == true && token['mode'] == mode, 'issued_mode_matches')
    expect(token['token'].is_a?(String) && !token['token'].empty?, 'issued_secret_present')
    expect(token['id'].is_a?(String) && !token['id'].empty?, 'issued_id_present')
    endpoint = URI.parse(token.fetch('endpoint_url'))
    expect(same_origin?(endpoint) && endpoint.path == '/mcp', 'issued_endpoint_origin_and_path')
    query = URI.decode_www_form(endpoint.query.to_s)
    expect(query == [['token', token['token']]], 'issued_endpoint_uses_its_own_secret')
    config = request(:get, '/api/mcp/config')
    expect(config['has_token'] == true, 'issued_token_marks_configured')
    expect(config['tokens'].any? { |item| item['id'] == token['id'] && item['mode'] == mode }, 'issued_token_listed')
    expect(!JSON.generate(config).include?(token['token']), 'config_never_reveals_issued_secret')
    token
  end

  def verify_read_client(token, auth)
    initialized = mcp_request(token, auth, rpc('initialize', {
      protocolVersion: '2025-06-18', capabilities: {},
      clientInfo: { name: 'isolated-http-acceptance', version: '1' }
    }))
    expect(initialized.dig('result', 'protocolVersion') == '2025-06-18', 'initialize_protocol')
    expect(initialized.dig('result', 'serverInfo', 'name') == 'quantmesh', 'initialize_server')
    expect(initialized.dig('result', 'capabilities', 'tools').is_a?(Hash), 'initialize_tools_capability')
    mcp_request(token, auth, { jsonrpc: '2.0', method: 'notifications/initialized' }, expected: 202)
    tools = tools_for(token, auth)
    expect(tools.any? { |name| name == READ_TOOL }, 'list_contains_read_tool')
    verify_success(call(token, READ_TOOL, auth))
  end

  def tools_for(token, auth)
    listed = mcp_request(token, auth, rpc('tools/list'))
    expect(listed.dig('result', 'tools').is_a?(Array), 'tools_list_is_array')
    listed.dig('result', 'tools').map { |tool| tool['name'] }
  end

  def verify_success(response)
    expect(response.is_a?(Hash) && response['jsonrpc'] == '2.0', 'jsonrpc_response_envelope')
    expect(response['error'].nil? && response.dig('result', 'isError') != true, 'tool_call_success')
    expect(response.dig('result', 'content', 0, 'text') == 'fixture completed', 'fake_handler_result')
  end

  def call(token, name, auth)
    mcp_request(token, auth, rpc('tools/call', { name: name, arguments: {} }))
  end

  def revoke(token)
    result = request(:delete, "/api/mcp/tokens/#{token.fetch('id')}")
    expect(result['ok'] == true, 'revoke_succeeds')
  end

  def verify_legacy
    legacy = { 'token' => LEGACY_TOKEN, 'endpoint_url' => BASE_URL + '/mcp?token=' + LEGACY_TOKEN }
    if SCENARIO == 'no_legacy'
      AUTH_STYLES.each { |auth| mcp_request(legacy, auth, rpc('initialize'), expected: 401) }
      return
    end
    before = state
    AUTH_STYLES.each do |auth|
      verify_read_client(legacy, auth)
      result = call(legacy, WRITE_A, auth)
      if SCENARIO == 'legacy_write'
        verify_success(result)
      else
        expect(result.dig('result', 'isError') == true, 'legacy_readonly_stays_readonly')
      end
    end
    write_delta = SCENARIO == 'legacy_write' ? AUTH_STYLES.length : 0
    after = wait_for_snapshot(before, 'writes' => write_delta, 'callbacks' => write_delta, 'notices_length' => 0)
    expect(after['writes'] == before['writes'] + write_delta, 'legacy_global_write_setting_preserved')
    expect(after['callbacks'] == before['callbacks'] + write_delta, 'legacy_observer_behavior_preserved')
    expect(after['notices'] == before['notices'], 'legacy_without_destination_has_no_notification')
  end

  def state
    snapshot = request(:get, '/__test/state')
    expect(snapshot['observer_timeout'] == false, 'observer_finished_within_deadline')
    expect(snapshot['observer_extra'] == false, 'observer_has_no_extra_completion')
    snapshot
  end

  # Closes the observer race: the fixture fires a notification asynchronously
  # after each accepted write, so the counters/notices can lag a brief moment
  # behind the HTTP response. Bounded-poll until every declared bar is met,
  # then return the final snapshot so the assertions below run on a settled
  # state (never on a guessed sleep).
  def wait_for_snapshot(base, bar)
    deadline = Time.now + 5.0
    last = nil
    until Time.now > deadline
      last = state
      callbacks_ok = last['callbacks'] >= base['callbacks'] + bar.fetch('callbacks', 0)
      notices_ok = last['notices'].length >= base['notices'].length + bar.fetch('notices_length', 0)
      writes_ok = bar.key?('writes') ? last['writes'] == base['writes'] + bar.fetch('writes') : true
      reads_ok = bar.key?('reads') ? last['reads'] == base['reads'] + bar.fetch('reads') : true
      failed_ok = bar.key?('failed_writes') ? last['failed_writes'] == base['failed_writes'] + bar.fetch('failed_writes') : true
      return last if callbacks_ok && notices_ok && writes_ok && reads_ok && failed_ok
      sleep 0.02
    end
    @check = 'observer_settled_before_timeout'
    assert false, 'observer_settled_before_timeout'
  end

  def mcp_request(token, auth, payload, expected: 200)
    if auth == :url
      request(:post, token.fetch('endpoint_url'), payload: payload, expected: expected)
    else
      request(:post, '/mcp', payload: payload, bearer: token.fetch('token'), expected: expected)
    end
  end

  def same_origin?(uri)
    uri.scheme == @base.scheme && uri.host == @base.host && uri.port == @base.port && uri.userinfo.nil?
  end

  def request(method, path, payload: nil, bearer: nil, expected: 200)
    uri = URI.join(BASE_URL, path)
    expect(same_origin?(uri), 'request_stays_on_isolated_fixture')
    http = Net::HTTP.new(uri.host, uri.port, nil) # Disable inherited proxy settings.
    http.open_timeout = 2
    http.read_timeout = 5
    http.write_timeout = 5
    http.max_retries = 0
    type = { get: Net::HTTP::Get, post: Net::HTTP::Post, delete: Net::HTTP::Delete }.fetch(method)
    message = type.new(uri.request_uri)
    message['Accept'] = 'application/json'
    message['Content-Type'] = 'application/json'
    message['Authorization'] = 'Bearer ' + bearer if bearer
    message.body = JSON.generate(payload) if payload
    response = http.start { |connection| connection.request(message) }
    expect(response.code.to_i == expected, "http_status_expected_#{expected}_got_#{response.code.to_i}")
    return {} if expected == 202

    @check = 'response_is_valid_json'
    JSON.parse(response.body)
  end
end

# Run Minitest without its default failure printer, which may print response
# values or URI-bearing exceptions. Reports contain only fixed stage/check names.
test = MCPTokenFlowTest.new('test_client_flow')
result = test.run
entry = {
  name: 'test_client_flow', status: result.passed? ? 'passed' : 'failed',
  assertions: result.assertions, stage: test.stage, check: test.check,
  failure_types: result.failures.map { |failure| failure.class.name }
}
report = { scenario: SCENARIO, transport: 'local_http', cases: [entry] }
File.write(File.join(REPORT_DIRECTORY, 'results.json'), JSON.pretty_generate(report) + "\n")
markdown = [
  '# MCP 令牌 HTTP 验收', '',
  "场景：#{SCENARIO}；传输：隔离的本地 HTTP。", '',
  '| 用例 | 结果 | 断言数 | 阶段 | 检查 |',
  '| --- | --- | --- | --- | --- |',
  "| #{entry[:name]} | #{entry[:status]} | #{entry[:assertions]} | #{entry[:stage]} | #{entry[:check]} |", '',
  '覆盖：颁发、URL/Bearer 鉴权、初始化、工具发现、只读限制、批量写通知、失败写入、吊销及旧令牌兼容。',
  '范围：仅假工具与内存设置；通知出口为本地记录，不验证真实账户、外部通知投递或网页交互。', ''
].join("\n")
File.write(File.join(REPORT_DIRECTORY, 'results.md'), markdown)
puts JSON.generate(scenario: SCENARIO, status: entry[:status], assertions: entry[:assertions], stage: entry[:stage], check: entry[:check])
exit(result.passed? ? 0 : 1)
