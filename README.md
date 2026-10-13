# Path Cache-Control

A Traefik v3 middleware plugin that sets a `Cache-Control` header on responses whose path starts with one of the configured prefixes (e.g. `/css/`, `/js/`, `/media/`), and leaves everything else untouched.

## Why

Traefik's built-in `headers` middleware has no path condition: it applies to every response of the router it is attached to. To give static assets a long browser cache without caching HTML pages, you usually need an extra router per site with a `PathPrefix` rule.

With this plugin, you define one middleware and attach it to the site's main router (or to an entrypoint).

## Behavior

- If the request is a `GET` or `HEAD`, its path starts with one of `pathPrefixes`, and the response status is `200`, `206` (range requests, e.g. videos) or `304`, the `Cache-Control` header sent by the backend is replaced by `cacheControl`.
- Everything else passes through unchanged: other methods (`POST`, `PUT`...), other paths, errors (`404`, `500`...), redirects, and look-alike paths (`/cssfoo/` does not match `/css/`).
- Prefixes are matched against the path only, so query strings like `?v=abc123` don't matter.
- Responses are not buffered: large files are streamed as usual and `Content-Length` is preserved.
- With an empty `pathPrefixes` or `cacheControl`, the middleware fails to start. Only the routers using it are affected (404 and an error in the Traefik logs), the others keep working.

## Configuration

| Option | Required | Default | Description |
|---|---|---|---|
| `pathPrefixes` | yes | none | List of path prefixes. Use a trailing slash (`/css/`) to match a folder only. |
| `cacheControl` | no | `public, max-age=2592000` | Value of the `Cache-Control` header (30 days by default). |

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

`test/integration/run.sh` starts a real Traefik (`v3.7.14` by default, override with `TRAEFIK_VERSION`) that loads this repository as a local plugin, in front of an nginx backend, then checks the responses with `curl`: headers on CSS, JS, media, `HEAD`, `206` and `304`, untouched pages, `POST`, `404` and redirects, a 5 MB file streamed with its `Content-Length`, and no error in the Traefik logs.

```bash
bash test/integration/run.sh
```

It needs Docker with Compose and uses port `18080`.
