# Path Cache-Control

[![CI](https://github.com/clementfavre/traefik-path-cache-control/actions/workflows/ci.yml/badge.svg)](https://github.com/clementfavre/traefik-path-cache-control/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A Traefik v3 middleware plugin that sets a `Cache-Control` header on responses whose path matches configured prefixes or file extensions (e.g. `/css/`, `/media/`, `.woff2`), and leaves everything else untouched.

## Why

This plugin came out of a real need. We run many websites behind Traefik and wanted browsers to keep static files (CSS, JS, images and other media) for a long time, while HTML pages stay fresh. We also wanted to do it at the proxy level, the same way for every site, instead of changing each application.

Traefik's built-in `headers` middleware has no path condition: it applies to every response of the router it is attached to. The native way is an extra router per site with a `PathPrefix` rule, which gets tedious to maintain across many sites. The plugins we found either could not match on the path or held whole responses in memory, which is not something you want on large media files.

So we wrote a small one that does just that. You define one middleware and attach it to the site's main router (or to an entrypoint).

## Behavior

- A rule matches a `GET` or `HEAD` request whose path starts with one of its `pathPrefixes` and/or ends with one of its `extensions`. When a rule has both, both must match. Rules are checked in order and the first match wins.
- If the response status is `200`, `206` (range requests, e.g. videos) or `304`, the `Cache-Control` header sent by the backend is replaced by the rule's `cacheControl`.
- Everything else passes through unchanged: other methods (`POST`, `PUT`...), other paths, errors (`404`, `500`...), redirects, and look-alike paths (`/cssfoo/` does not match `/css/`).
- Matching is case-sensitive by default. Set `caseInsensitive: true` for backends that ignore case, like IIS (`/Media/Photo.JPG` then matches `/media/` and `.jpg`).
- Only the path is matched, so query strings like `?v=abc123` don't matter.
- Responses are not buffered: large files are streamed as usual and `Content-Length` is preserved.
- An invalid configuration (nothing to match, empty value, empty `cacheControl`...) makes the middleware fail to start. Only the routers using it are affected (404 and an error in the Traefik logs), the others keep working.

## Configuration

| Option | Default | Description |
|---|---|---|
| `pathPrefixes` | none | Shorthand for a single rule: list of path prefixes. Use a trailing slash (`/css/`) to match a folder only. |
| `extensions` | none | Shorthand for a single rule: list of file extensions, with or without the dot (`.woff2` or `woff2`). |
| `cacheControl` | `public, max-age=2592000` | Value of the `Cache-Control` header (30 days). Also the default for rules without their own `cacheControl`. |
| `caseInsensitive` | `false` | Match prefixes and extensions regardless of case. |
| `rules` | none | List of rules, each with `pathPrefixes`, `extensions` and an optional `cacheControl`. Cannot be combined with the top-level `pathPrefixes` / `extensions`. |

At least one of `pathPrefixes`, `extensions` or `rules` is required.

## Installation

### From the plugin catalog

Static configuration (`traefik.yml`):

```yaml
experimental:
  plugins:
    pathcachecontrol:
      moduleName: github.com/clementfavre/traefik-path-cache-control
      version: v0.1.0
```

Traefik downloads the plugin at startup, so it needs outbound internet access. Restart Traefik after changing the static configuration.

### As a local plugin

Copy `.traefik.yml`, `go.mod` and `pathcachecontrol.go` to:

```
plugins-local/src/github.com/clementfavre/traefik-path-cache-control/
```

Mount it in the Traefik container (the official image looks for local plugins in `/plugins-local`):

```yaml
volumes:
  - ./plugins-local:/plugins-local:ro
```

Static configuration:

```yaml
experimental:
  localPlugins:
    pathcachecontrol:
      moduleName: github.com/clementfavre/traefik-path-cache-control
```

Restart Traefik. With `log.level: INFO`, the logs show `Plugins loaded. plugins=["pathcachecontrol"]`.

## Usage

Dynamic configuration (file provider):

```yaml
http:
  middlewares:
    static-cache:
      plugin:
        pathcachecontrol:
          pathPrefixes:
            - /css/
            - /js/
            - /media/
          cacheControl: "public, max-age=2592000"

  routers:
    my-site:
      rule: Host(`www.example.com`)
      middlewares:
        - static-cache
      service: my-site
```

With rules, to use a different duration per kind of file:

```yaml
http:
  middlewares:
    static-cache:
      plugin:
        pathcachecontrol:
          caseInsensitive: true
          # Default for the rules below
          cacheControl: "public, max-age=2592000"
          rules:
            # Versioned assets: one year
            - pathPrefixes:
                - /css/
                - /js/
              cacheControl: "public, max-age=31536000, immutable"
            # Media: default (30 days)
            - pathPrefixes:
                - /media/
            # Fonts, wherever they are
            - extensions:
                - .woff2
                - .woff
```

Only use `immutable` on files whose URL changes with each release, the browser won't even revalidate them.

To apply it to every site behind an entrypoint, add it to the entrypoint instead (static configuration):

```yaml
entryPoints:
  websecure:
    address: ":443"
    http:
      middlewares:
        - static-cache@file
```

> [!WARNING]
> Make sure your CSS and JS URLs change with each release (content hash in the file name, or a version query string such as ASP.NET Core's `asp-append-version="true"`). Otherwise returning visitors keep the old files until the cache expires.

## Check

```bash
curl -s -o /dev/null -D - https://www.example.com/css/main.css | grep -i cache-control
# Cache-Control: public, max-age=2592000
```

On Windows, use `curl.exe -s -o NUL -D - <url>`.

## Development

Run the tests in a Go container:

```bash
docker run --rm -v "$PWD:/src" -w /src golang:1.22-alpine go test -v ./...
```

Traefik runs plugins with the [Yaegi](https://github.com/traefik/yaegi) interpreter, not the Go compiler, so the tests also run under Yaegi in CI (see `.github/workflows/ci.yml`).

### Integration test

`test/integration/run.sh` starts a real Traefik (`v3.7.14` by default, override with `TRAEFIK_VERSION`) that loads this repository as a local plugin, in front of an nginx backend, then checks the responses with `curl`: headers on CSS, JS, media, `HEAD`, `206` and `304`, untouched pages, `POST`, `404` and redirects, rules with their own or the default `cacheControl`, extensions, case insensitive matching, a 5 MB file streamed with its `Content-Length`, and no error in the Traefik logs.

```bash
bash test/integration/run.sh
```

It needs Docker with Compose and uses port `18080`.

## License

MIT, see [LICENSE](LICENSE).
