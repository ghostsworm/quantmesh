#!/usr/bin/env ruby
# Full source + test TypeScript diagnostics. Nonzero compiler status stays nonzero.
require 'json'
require 'open3'
require 'fileutils'
require 'time'

output_dir = File.expand_path(ARGV.fetch(0) { abort 'Usage: ruby scripts/verify_frontend_types.rb OUTPUT_DIRECTORY' })
root = File.expand_path('..', __dir__)
command = ['yarn', 'typecheck', '--pretty', 'false']
stdout, stderr, status = Open3.capture3(*command, chdir: File.join(root, 'webui'))
diagnostics = stdout.lines.filter_map do |line|
  match = line.match(/^(.+)\((\d+),(\d+)\): error (TS\d+): (.*)$/)
  next unless match
  { file: match[1], line: match[2].to_i, column: match[3].to_i, code: match[4], message: match[5] }
end
report = {
  generated_at: Time.now.iso8601, command: command, exit_code: status.exitstatus,
  error_count: diagnostics.length, errors_by_file: diagnostics.group_by { |d| d[:file] }.transform_values(&:length),
  diagnostics: diagnostics, stdout: stdout, stderr: stderr,
  scope: 'All webui/src TypeScript and TSX, including tests. Strict checking; dependency declarations skipped. No source exclusions.'
}
FileUtils.mkdir_p(output_dir)
File.write(File.join(output_dir, 'types.json'), JSON.pretty_generate(report) + "\n")
markdown = ['# 前端完整类型检查', '', "时间：#{report[:generated_at]}", '',
            "退出码：#{report[:exit_code]}；诊断数：#{report[:error_count]}", '', report[:scope], '',
            '| 文件 | 类型错误数 |', '|---|---|']
report[:errors_by_file].each { |file, count| markdown << "| #{file} | #{count} |" }
markdown.concat(['', '```text', stdout, stderr, '```'])
File.write(File.join(output_dir, 'types.md'), markdown.join("\n") + "\n")
puts JSON.generate(report.slice(:exit_code, :error_count, :errors_by_file))
exit(status.exitstatus || 1)
