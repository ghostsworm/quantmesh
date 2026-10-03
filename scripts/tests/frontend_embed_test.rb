require 'minitest/autorun'
require 'open3'
require_relative '../frontend_embed'

class FrontendEmbedTest < Minitest::Test
  def setup
    @root = Dir.mktmpdir('quantmesh-embed-fixture-')
    FileUtils.mkdir_p(File.join(@root, 'webui', 'dist', 'assets'))
    FileUtils.mkdir_p(File.join(@root, 'webui', 'src'))
    FileUtils.mkdir_p(File.join(@root, 'web'))
    FileUtils.mkdir_p(File.join(@root, 'scripts'))
    FileUtils.copy_file(File.expand_path('../frontend_embed.rb', __dir__), File.join(@root, 'scripts', 'frontend_embed.rb'))
    write('main.go', "var Version = \"1.2.3-rc1\"\n")
    write('webui/package.json', JSON.generate(version: '1.2.3-rc1'))
    write('webui/src/app.ts', 'fixture source')
    write('webui/dist/index.html', '<script src="/assets/app.js"></script>')
    write('webui/dist/assets/app.js', 'fixture bundle')
    @gate = FrontendEmbed::Gate.new(@root)
    stamp
  end

  def teardown
    FileUtils.remove_entry_secure(@root)
  end

  def write(name, value)
    File.write(File.join(@root, name), value)
  end

  def stamp
    metadata = { schema: 1, version: @gate.version, source_digest: @gate.source_digest, files: @gate.bundle_files(File.join(@root, 'webui/dist')) }
    write('webui/dist/build-meta.json', JSON.generate(metadata))
  end

  def test_complete_bundle_round_trip
    @gate.sync
    assert_equal '1.2.3-rc1', @gate.verify['version']
    assert_equal @gate.files(File.join(@root, 'webui/dist')), @gate.files(File.join(@root, 'web/dist'))
  end

  def test_rejects_stale_source_and_keeps_old_destination
    FileUtils.mkdir_p(File.join(@root, 'web/dist'))
    write('web/dist/old.js', 'preserve')
    write('webui/src/app.ts', 'changed')
    assert_raises(FrontendEmbed::Invalid) { @gate.sync }
    assert_equal 'preserve', File.read(File.join(@root, 'web/dist/old.js'))
  end

  def test_rejects_version_mismatch
    write('main.go', "var Version = \"1.2.3-rc2\"\n")
    assert_raises(FrontendEmbed::Invalid) { @gate.sync }
  end

  def test_rejects_mutated_bundle_missing_asset_or_extra_file
    %w[mutated missing extra].each do |mode|
      setup if mode != 'mutated'
      case mode
      when 'mutated' then write('webui/dist/assets/app.js', 'wrong')
      when 'missing' then File.unlink(File.join(@root, 'webui/dist/assets/app.js'))
      when 'extra' then write('webui/dist/obsolete.js', 'old')
      end
      assert_raises(FrontendEmbed::Invalid) { @gate.sync }
      teardown if mode != 'extra'
    end
  end

  def test_rejects_missing_marker_and_broken_index_reference
    File.unlink(File.join(@root, 'webui/dist/build-meta.json'))
    assert_raises(Errno::ENOENT) { @gate.sync }
    write('webui/dist/index.html', '<script src="/assets/missing.js"></script>')
    assert_raises(FrontendEmbed::Invalid) { stamp }
  end

  def test_rejects_symlinked_destination_without_writing_outside
    outside = Dir.mktmpdir('quantmesh-outside-fixture-')
    File.symlink(outside, File.join(@root, 'web/dist'))
    assert_raises(FrontendEmbed::Invalid) { @gate.sync }
    assert_empty Dir.children(outside)
  ensure
    FileUtils.remove_entry_secure(outside) if outside
  end

  def test_copy_failure_preserves_old_destination
    FileUtils.mkdir_p(File.join(@root, 'web/dist'))
    write('web/dist/old.js', 'preserve')
    FileUtils.stub(:copy_file, proc { raise IOError, 'injected copy failure' }) do
      assert_raises(IOError) { @gate.sync }
    end
    assert_equal 'preserve', File.read(File.join(@root, 'web/dist/old.js'))
  end

  def test_failed_build_invalidates_old_marker
    @gate.define_singleton_method(:system) { |*| false }
    assert_raises(FrontendEmbed::Invalid) { @gate.build }
    refute File.exist?(File.join(@root, 'webui/dist/build-meta.json'))
  end

  def test_source_changes_during_build_cannot_receive_verified_marker
    root = @root
    @gate.define_singleton_method(:system) do |*|
      File.write(File.join(root, 'webui/src/app.ts'), 'changed during build')
      true
    end
    assert_raises(FrontendEmbed::Invalid) { @gate.build }
    refute File.exist?(File.join(@root, 'webui/dist/build-meta.json'))
  end

  def test_rejects_symlinked_metadata
    marker = File.join(@root, 'webui/dist/build-meta.json')
    File.rename(marker, File.join(@root, 'metadata.json'))
    File.symlink(File.join(@root, 'metadata.json'), marker)
    assert_raises(FrontendEmbed::Invalid) { @gate.sync }
  end

  def test_hashes_internal_source_file_symlink_and_target_changes
    File.symlink('app.ts', File.join(@root, 'webui/src/icon.ts'))
    before = @gate.source_digest
    write('webui/src/app.ts', 'changed target')
    refute_equal before, @gate.source_digest
  end

  def test_rejects_source_symlink_escaping_source_directory
    File.symlink('../../main.go', File.join(@root, 'webui/src/outside.ts'))
    assert_raises(FrontendEmbed::Invalid) { @gate.source_digest }
  end

  def test_failed_destination_swap_restores_old_bundle
    FileUtils.mkdir_p(File.join(@root, 'web/dist'))
    write('web/dist/old.js', 'preserve')
    original = File.method(:rename)
    File.stub(:rename, proc { |source, target|
      raise IOError, 'injected swap failure' if File.basename(source).start_with?('.embed-stage-')
      original.call(source, target)
    }) do
      assert_raises(IOError) { @gate.sync }
    end
    assert_equal 'preserve', File.read(File.join(@root, 'web/dist/old.js'))
  end

  def test_parallel_make_never_builds_backend_after_frontend_failure
    FileUtils.copy_file(File.expand_path('../../Makefile', __dir__), File.join(@root, 'Makefile'))
    FileUtils.mkdir_p(File.join(@root, 'webui/node_modules'))
    FileUtils.mkdir_p(File.join(@root, 'bin'))
    write('bin/yarn', "#!/bin/sh\nexit 7\n")
    write('bin/go', "#!/bin/sh\ntouch backend-was-built\n")
    %w[yarn go].each { |name| File.chmod(0o755, File.join(@root, 'bin', name)) }
    _, _, status = Open3.capture3({ 'PATH' => File.join(@root, 'bin') + ':' + ENV.fetch('PATH') }, 'make', '-j4', 'build', chdir: @root)
    refute status.success?
    refute File.exist?(File.join(@root, 'backend-was-built'))
    refute File.exist?(File.join(@root, 'web/dist'))
  end
end
