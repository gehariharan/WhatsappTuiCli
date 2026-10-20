# Private release downloads use the user's existing GitHub CLI authentication.
# No access token is embedded in the cask, URL, cache metadata, or command line.
require "download_strategy"

class WasyncGitHubReleaseDownloadStrategy < CurlDownloadStrategy
  def fetch(timeout: nil)
    match = %r{\Ahttps://github\.com/(?<repo>gehariharan/WhatsappTuiCli)/releases/download/(?<tag>v[0-9A-Za-z.-]+)/(?<asset>wasync_[0-9A-Za-z._-]+\.tar\.gz)\z}.match(url)
    raise "Unexpected wasync release URL" unless match

    download_lock = DownloadLock.new(temporary_path)
    begin
      download_lock.lock_or_wait(quiet: quiet?, timeout: timeout)
      unless cached_location.exist?
        gh = HOMEBREW_PREFIX/"bin/gh"
        raise "Install gh and run gh auth login before downloading private wasync releases" unless gh.executable?

        cached_location.dirname.mkpath
        token = ENV["HOMEBREW_GITHUB_API_TOKEN"]
        child_env = token.to_s.empty? ? {} : { "GH_TOKEN" => token }
        system_command! gh,
                        args: ["release", "download", match[:tag], "--repo", match[:repo],
                               "--pattern", match[:asset], "--output", temporary_path.to_s, "--clobber"],
                        env: child_env, secrets: [token].compact, timeout: timeout, print_stderr: true
        temporary_path.rename(cached_location)
      end
      create_symlink_to_cached_download(cached_location)
    ensure
      download_lock.unlock(unlink: true)
    end
  end

  private

  # Avoid CurlDownloadStrategy's unauthenticated HEAD request for private assets.
  def resolved_url_and_basename
    [url, File.basename(URI(url).path)]
  end
end
