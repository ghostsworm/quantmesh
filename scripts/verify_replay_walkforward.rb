#!/usr/bin/env ruby
# Isolated replay experiment runner. Writes JSON and Markdown evidence together.
require 'json'
require 'open3'
require 'fileutils'
require 'digest'
require 'time'
require_relative 'source_provenance'

abort 'Usage: ruby scripts/verify_replay_walkforward.rb NEW_OUTPUT_DIRECTORY' unless ARGV.length == 1
root = File.expand_path('..', __dir__)
output = File.expand_path(ARGV.first)
abort "Output directory already exists: #{output}" if File.exist?(output)
FileUtils.mkdir_p(output)
source_before = SourceProvenance.capture(root)
checks = []
commands = [
  ['go', 'test', './backtest/optimizer', './backtest/replay', './backtest/optimrun', './storage', './strategy/regime', '-run', 'Replay|BuildWalkForwardWindows|ClosedKlineSourceWarmup|EngineSleep|FeatureHooksSleep|ParseKlineIntervalRejectsOverflow', '-count=1', '-json'],
  ['go', 'test', './web/api_backtest_optim.go', './web/replay_optimizer_api_test.go', '-count=1', '-json'],
  ['go', 'run', './tools/replaywf', '--output', File.join(output, 'samples')]
]
commands.each_with_index do |command, index|
  stdout, stderr, status = Open3.capture3({ 'REPLAY_OPTIMIZER_REPORT_DIR' => File.join(output, 'api') }, *command, chdir: root)
  File.write(File.join(output, "check-#{index}.log"), stdout + stderr)
  checks << { command: command, exit_code: status.exitstatus, log: "check-#{index}.log" }
  break unless status.success?
end
samples = %w[rising sideways falling].filter_map do |regime|
  path = File.join(output, 'samples', "#{regime}-result.json")
  next unless File.file?(path)
  result = JSON.parse(File.read(path))
  experiment = result.fetch('replay_walk_forward')
  { regime: regime, result_file: "samples/#{regime}-result.json", data_file: "samples/#{regime}-candles.json",
    data_file_sha256: Digest::SHA256.file(File.join(output, 'samples', "#{regime}-candles.json")).hexdigest,
    experiment: experiment }
end
source_after = SourceProvenance.capture(root)
# Output should live in an ignored directory (e.g. tmp/), otherwise it itself
# changes the source snapshot. Never pretend that mismatched snapshots agree.
source_stable = source_before[:source_tree_sha256] == source_after[:source_tree_sha256]
passed = checks.length == commands.length && checks.all? { |c| c[:exit_code] == 0 } && samples.length == 3 && source_stable
report = { generated_at: Time.now.utc.iso8601, passed: passed, source: source_before, source_stable: source_stable,
           scope: 'Deterministic synthetic grid replay only; no real accounts, market profitability claim, deployment or release.',
           checks: checks, samples: samples }
File.write(File.join(output, 'report.json'), JSON.pretty_generate(report) + "\n")
md = ['# Replay 网格样本外验证', '', "状态：#{passed ? '通过' : '失败'}；源码快照稳定：#{source_stable}", '',
      "基线提交：`#{source_before[:baseline_commit]}`（dirty=#{source_before[:source_dirty]}）", '',
      "工作树摘要：`#{source_before[:source_tree_sha256]}`", '',
      '数据：固定公式生成的上涨、横盘、下跌 OHLCV，非真实市场。每折训练 3 天、测试 1 天，向前滚动 1 天。', '',
      '训练按净收益率减最大回撤选参，仅冻结优胜参数进入后续测试；基线固定。各窗独立空仓、同额本金；预热只用窗内已闭合 K 线，期末按市值计价，不强平、不跨窗复利。', '',
      '| 行情 | 折 | 区间 | 净收益率 % | 最大回撤 % | 成交回报数 | 手续费 | 资金费支出 | 未实现盈亏 |',
      '|---|---:|---|---:|---:|---:|---:|---:|---:|']
samples.each do |sample|
  sample[:experiment].fetch('folds').each do |fold|
    { 'train' => '样本内', 'test' => '样本外', 'baseline_train' => '基线样本内', 'baseline_test' => '基线样本外' }.each do |key, label|
      e = fold.fetch(key)
      md << format('| %s | %d | %s | %.6f | %.6f | %d | %.6f | %.6f | %.6f |', sample[:regime], fold.fetch('index') + 1, label,
                   e.fetch('total_return'), e.fetch('max_drawdown'), e.fetch('fills'), e.fetch('fees_total'), e.fetch('funding_paid'), e.fetch('unrealized_pnl'))
    end
  end
end
md.concat(['', '## 冻结摘要', ''])
samples.each do |sample|
  e = sample[:experiment]
  md << "- #{sample[:regime]}：数据 `#{e.fetch('data_sha256')}`；实验 `#{e.fetch('experiment_sha256')}`；原始 K 线文件 `#{sample[:data_file]}`。"
end
md.concat(['', '## 边界与负例', '',
           '- 改写测试窗及以后价格/成交量：训练指标、候选排序和冻结参数必须不变；数据/实验摘要必须改变。',
           '- 拒绝重复时间戳、缺口、周期不匹配、跨越边界的半根 K 线、重叠测试窗和超限搜索；调用方修改原始嵌套参数/行情也不能改写已冻结实验。',
           '- 缺历史盘口、历史预测资金费率；K 线内路径和成交量分配为假设，零延迟、无冲击成本。手续费与结算资金费已计入，未建模滑点不视为已证实零成本。',
           '- 不允许用最终结算费率作提前可知的定价/偏置信号；本入口只使用资金费结算成本。',
           '- 本报告不代表真实行情样本外通过、实盘盈利、FundingCarry 验收、部署或发布。', '', '## 执行记录', ''])
checks.each { |c| md << "- `#{c[:command].join(' ')}`：退出码 #{c[:exit_code]}，输出 `#{c[:log]}`。" }
File.write(File.join(output, 'report.md'), md.join("\n") + "\n")
puts JSON.generate({ passed: passed, source_stable: source_stable, samples: samples.length, report: File.join(output, 'report.json') })
exit(passed ? 0 : 1)
