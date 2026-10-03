#!/usr/bin/env ruby
require 'json'
require 'digest'
require 'fileutils'
require 'tmpdir'
require 'uri'

module FrontendEmbed
  MARKER = 'build-meta.json'.freeze
  class Invalid < StandardError; end
  class Gate
    def initialize(root)
      @root = File.realpath(root)
      @ui = File.join(@root, 'webui')
      @dist = File.join(@ui, 'dist')
      @embedded = File.join(@root, 'web', 'dist')
      raise Invalid, 'frontend directory absent or symlinked' unless File.directory?(@ui) && !File.symlink?(@ui)
    end

    def version
      frontend = JSON.parse(File.read(File.join(@ui, 'package.json'))).fetch('version')
      backend = File.read(File.join(@root, 'main.go'))[/^var Version = "([^"]+)"$/, 1]
      raise Invalid, 'frontend/backend versions disagree' unless frontend == backend && frontend.match?(/\A\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?\z/)
      frontend
    end

    def files(directory, source_links: false)
      raise Invalid, 'bundle directory absent or symlinked' unless File.directory?(directory) && !File.symlink?(directory)
      Dir.glob(File.join(directory, '**', '*'), File::FNM_DOTMATCH).sort.filter_map do |path|
        next if ['.', '..'].include?(File.basename(path))
        stat = File.lstat(path)
        if stat.symlink? && source_links
          target = File.realpath(path)
          raise Invalid, 'source symlink escapes directory or targets non-file' unless target.start_with?(File.realpath(directory) + '/') && File.file?(target)
          hash = Digest::SHA256.hexdigest(JSON.generate([File.readlink(path), Digest::SHA256.file(target).hexdigest]))
          next [path.delete_prefix(directory + '/'), hash]
        end
        raise Invalid, 'symlink or special file in build input' unless stat.file? || stat.directory?
        next unless stat.file?
        [path.delete_prefix(directory + '/'), Digest::SHA256.file(path).hexdigest]
      end.to_h
    end

    def source_digest
      inputs = {}
      %w[src public].each do |dir|
        path = File.join(@ui, dir)
        files(path, source_links: true).each { |name, hash| inputs[dir + '/' + name] = hash } if File.exist?(path)
      end
      Dir.children(@ui).sort.each do |name|
        next unless name.match?(/\.(?:json|js|ts|html|lock|yml)$/)
        path = File.join(@ui, name)
        raise Invalid, 'symlinked frontend configuration' if File.symlink?(path)
        next if File.directory?(path)
        inputs[name] = Digest::SHA256.file(path).hexdigest
      end
      inputs['../scripts/frontend_embed.rb'] = Digest::SHA256.file(File.join(@root, 'scripts', 'frontend_embed.rb')).hexdigest
      Digest::SHA256.hexdigest(JSON.generate(inputs.sort.to_h))
    end

    def bundle_files(directory)
      entries = files(directory).reject { |name, _| name == MARKER }
      raise Invalid, 'missing index or JavaScript assets' unless entries.key?('index.html') && entries.keys.any? { |name| name.start_with?('assets/') && name.end_with?('.js') }
      File.read(File.join(directory, 'index.html')).scan(/(?:src|href)=["']([^"']+)["']/).flatten.each do |ref|
        uri = URI.parse(ref)
        next if uri.scheme || uri.host || ref.start_with?('#')
        name = uri.path.sub(%r{\A/}, '').sub(%r{\A\./}, '')
        raise Invalid, 'index references absent or escaping asset' if name.split('/').include?('..') || !entries.key?(name)
      end
      entries
    end

    def build
      raise Invalid, 'frontend output symlinked' if File.symlink?(@dist)
      expected_version, before = version, source_digest
      if File.file?(File.join(@dist, MARKER))
        quarantine = Dir.mktmpdir('quantmesh-old-frontend-marker-')
        File.rename(File.join(@dist, MARKER), File.join(quarantine, MARKER))
      end
      raise Invalid, 'frontend build failed; no verified bundle produced' unless system('yarn', 'build:assets', chdir: @ui)
      raise Invalid, 'frontend sources changed during build' unless before == source_digest && expected_version == version
      metadata = { 'schema' => 1, 'version' => expected_version, 'source_digest' => before, 'files' => bundle_files(@dist) }
      File.write(File.join(@dist, MARKER), JSON.pretty_generate(metadata) + "\n")
      verify(@dist)
    end

    def verify(directory = @embedded)
      files(directory) # reject symlinks before opening metadata
      metadata = JSON.parse(File.read(File.join(directory, MARKER)))
      raise Invalid, 'stale frontend provenance' unless metadata['schema'] == 1 && metadata['version'] == version && metadata['source_digest'] == source_digest
      raise Invalid, 'bundle files changed or incomplete' unless metadata['files'] == bundle_files(directory)
      metadata
    end

    def sync
      expected = verify(@dist)
      web = File.join(@root, 'web')
      raise Invalid, 'web directory absent or symlinked' unless File.directory?(web) && !File.symlink?(web)
      raise Invalid, 'embedded destination symlinked' if File.symlink?(@embedded)
      stage = Dir.mktmpdir('.embed-stage-', web)
      expected['files'].keys.push(MARKER).each do |name|
        target = File.join(stage, name)
        FileUtils.mkdir_p(File.dirname(target))
        FileUtils.copy_file(File.join(@dist, name), target)
      end
      raise Invalid, 'bundle changed during copy' unless verify(stage) == expected && verify(@dist) == expected
      backup = nil
      if File.exist?(@embedded)
        backup = File.join(Dir.mktmpdir('quantmesh-embedded-backup-'), 'dist')
        File.rename(@embedded, backup)
      end
      begin
        File.rename(stage, @embedded)
      rescue StandardError
        File.rename(backup, @embedded) if backup && !File.exist?(@embedded)
        raise
      end
      puts "Previous embedded bundle retained: #{backup}" if backup
      verify
    ensure
      FileUtils.remove_entry_secure(stage) if stage && File.directory?(stage)
    end
  end
end

if $PROGRAM_NAME == __FILE__
  begin
    gate = FrontendEmbed::Gate.new(File.expand_path('..', __dir__))
    case ARGV.fetch(0, '')
    when 'build' then gate.build
    when 'sync' then gate.sync
    when 'verify' then gate.verify
    when 'version' then puts gate.version
    else raise FrontendEmbed::Invalid, 'usage: frontend_embed.rb build|sync|verify|version'
    end
  rescue StandardError => e
    warn "Frontend embed gate failed (#{e.class}): #{e.message}"
    exit 1
  end
end
