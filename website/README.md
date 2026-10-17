# wasync installer page

Product page for `https://gehariharan.com/wasync/`. A dedicated Cloudflare Worker
serves only `/wasync` and `/wasync/*` on the existing domain and its `www` host.
The blog remains served by its existing Worker. This source uses the live site's
light/dark palette, system fonts, serif headings and navigation.

The page and assets are static. `worker.mjs` resolves download links using the
public GitHub Releases API, including prereleases, sorted by publication date.
Only releases with all four platform assets are offered; early unsigned previews
are excluded. Metadata is cached for two minutes. New installer releases become
available through the same links without redeploying the page. No credentials,
WhatsApp sessions, archive data or user databases are used by this website.

## Preview

With Wrangler installed:

```sh
wrangler dev --local --config website/wrangler.jsonc --port 18931
```

Open `http://localhost:18931/wasync/`. Static assets are in `public/wasync/`.
The terminal illustration contains synthetic group names and message counts.

## Deploy

Authenticate with the Cloudflare account that owns `gehariharan.com`, then deploy:

```sh
wrangler login
wrangler deploy --config website/wrangler.jsonc
```

If a shell exports an API token for a different account, unset it for these
commands using `env -u CLOUDFLARE_API_TOKEN -u CLOUDFLARE_ACCOUNT_ID`. When multiple
accounts are available, select the domain's account explicitly. Never commit
Wrangler state or API tokens. Application releases use a separate GitHub workflow;
the website's release links track those publications automatically.
