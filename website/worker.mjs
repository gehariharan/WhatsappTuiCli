const repository = "gehariharan/WhatsappTuiCli";
const github = `https://github.com/${repository}`;
const versionPattern = /^v([0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9][A-Za-z0-9.-]*)?)$/;

async function releaseInfo(request, context) {
  const cacheURL = new URL("/wasync/releases.json", request.url);
  cacheURL.hostname = "gehariharan.com";
  const cacheKey = new Request(cacheURL);
  const cached = await caches.default.match(cacheKey);
  if (cached) return cached.json();
  const response = await fetch(`https://api.github.com/repos/${repository}/releases?per_page=20`, {
    headers: { Accept: "application/vnd.github+json", "User-Agent": "wasync-installer-site" },
    signal: AbortSignal.timeout(10000),
  });
  if (!response.ok) throw new Error("Release service is temporarily unavailable");
  const releases = await response.json();
  // GitHub's list ordering can differ for prereleases; choose by publication.
  const release = releases.sort((a, b) => (b.published_at || "").localeCompare(a.published_at || "")).find(item => {
    const match = versionPattern.exec(item.tag_name || "");
    if (item.draft || !match) return false;
    // The early numbered previews were unsigned. Never offer those installers.
    const early = /^0\.1\.0-preview\.([0-9]+)$/.exec(match[1]);
    if (early && BigInt(early[1]) < 5n) return false;
    return ["darwin_arm64.pkg", "darwin_amd64.pkg", "linux_arm64.tar.gz", "linux_amd64.tar.gz"]
      .every(suffix => item.assets?.some(asset => asset.name === `wasync_${match[1]}_${suffix}`));
  });
  if (!release) throw new Error("No published installer is available yet");
  const version = versionPattern.exec(release.tag_name)[1];
  const info = {
    version,
    published_at: release.published_at,
    html_url: `${github}/releases/tag/v${version}`,
    downloads: Object.fromEntries([
      ["darwin-arm64", "darwin_arm64.pkg"], ["darwin-amd64", "darwin_amd64.pkg"],
      ["linux-arm64", "linux_arm64.tar.gz"], ["linux-amd64", "linux_amd64.tar.gz"],
    ].map(([key, suffix]) => [key, `${github}/releases/download/v${version}/wasync_${version}_${suffix}`])),
    checksums_url: `${github}/releases/download/v${version}/checksums.txt`,
  };
  context.waitUntil(caches.default.put(cacheKey, Response.json(info, {
    headers: { "Cache-Control": "public, max-age=120" },
  })));
  return info;
}

export default {
  async fetch(request, env, context) {
    const url = new URL(request.url);
    const download = /^\/wasync\/download\/(darwin-arm64|darwin-amd64|linux-arm64|linux-amd64)$/.exec(url.pathname);
    if (url.pathname !== "/wasync/releases.json" && !download) return env.ASSETS.fetch(request);
    if (!["GET", "HEAD"].includes(request.method)) return new Response("Method not allowed", { status: 405, headers: { Allow: "GET, HEAD" } });
    try {
      const info = await releaseInfo(request, context);
      if (download) return new Response(null, { status: 302, headers: { Location: info.downloads[download[1]], "Cache-Control": "public, max-age=60" } });
      return new Response(request.method === "HEAD" ? null : JSON.stringify(info), {
        headers: { "Content-Type": "application/json; charset=utf-8", "Cache-Control": "public, max-age=120" },
      });
    } catch {
      if (download) return Response.redirect(`${github}/releases`, 302);
      return Response.json({ error: "Release information is temporarily unavailable", html_url: `${github}/releases` }, { status: 503 });
    }
  },
};
