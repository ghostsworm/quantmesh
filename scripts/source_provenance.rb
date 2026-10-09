require 'digest'
require 'json'
require 'open3'

module SourceProvenance
  def self.capture(root)
    sha, _, sha_status = Open3.capture3('git', 'rev-parse', 'HEAD', chdir: root)
    worktree, _, worktree_status = Open3.capture3('git', 'status', '--short', '--untracked-files=all', chdir: root)
    tracked_diff, _, diff_status = Open3.capture3('git', 'diff', '--binary', '--no-ext-diff', '--no-textconv', '--full-index', 'HEAD', '--', chdir: root)
    untracked_paths, _, untracked_status = Open3.capture3('git', 'ls-files', '--others', '--exclude-standard', '-z', chdir: root)

    digest = Digest::SHA256.new
    digest.update("tracked-diff\0")
    digest.update(tracked_diff)
    untracked_paths.split("\0").sort.each do |relative_path|
      path = File.join(root, relative_path)
      digest.update("untracked\0#{relative_path.bytesize}:#{relative_path}\0")
      begin
        stat = File.lstat(path)
        digest.update("#{stat.ftype}\0#{stat.mode & 0o111}\0")
        if stat.symlink?
          digest.update(File.readlink(path))
        elsif stat.file?
          File.open(path, 'rb') do |file|
            while (chunk = file.read(1024 * 1024))
              digest.update(chunk)
            end
          end
        else
          digest.update('unsupported-file-type')
        end
      rescue SystemCallError => error
        digest.update("unreadable:#{error.class}:#{error.message}")
      end
      digest.update("\0")
    end

    frontend_version = JSON.parse(File.read(File.join(root, 'webui/package.json'))).fetch('version')
    server_source = File.read(File.join(root, 'main.go'))
    server_version_match = server_source.match(/^var Version = "([^"]+)"/)
    server_version = server_version_match && server_version_match[1]
    complete = !!(sha_status.success? && worktree_status.success? && diff_status.success? && untracked_status.success? && server_version)

    {
      baseline_commit: sha.strip,
      worktree_status: worktree,
      source_dirty: !worktree.empty?,
      source_tree_sha256: digest.hexdigest,
      server_version: server_version,
      frontend_version: frontend_version,
      source_provenance_complete: complete,
      versions_consistent: !!(complete && server_version == frontend_version)
    }
  end
end
