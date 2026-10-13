# onepassword-mcp-plugin

A standalone gateway that lets MCP users stop pasting upstream credentials into their
client configuration. Users keep their own credentials (a GitLab token, a Gerrit HTTP
password, an Atlassian API token) in 1Password. The MCP client config holds only the
service URL. The gateway authenticates each request through
[mcp-auth](https://github.com/mcp-runtime/mcp-auth), looks up **that user's** credentials for **that service**, and
injects them into the request it forwards to the real MCP server.

* mcp-auth stays the only OAuth authorization server. This plugin is **not** one.
* Upstream credentials are **never** returned to the MCP client or the OAuth flow.
* mcp-auth has no 1Password dependency. The 1Password SDK and service-account token live
  only in this plugin.

Versioned and released independently of mcp-auth (tags `vX.Y.Z`, image
`ghcr.io/mcp-runtime/onepassword-mcp-plugin`). It depends only on mcp-auth's resource-server SDK
(`auth-client/go`), never on `auth-server` code (enforced by `internal/boundary_test.go`).

**Requires** an mcp-auth with external credential connector support
([mcp-runtime/mcp-auth#24](https://github.com/mcp-runtime/mcp-auth/pull/24), closes
[#23](https://github.com/mcp-runtime/mcp-auth/issues/23)). Until that is released, `go.mod` pins the SDK
to the PR commit; bump it to the release tag after merge.

## Status

| Area | State |
| --- | --- |
| Generic gateway, OAuth challenge and metadata, per-request authorisation, credential injection (POST/GET/DELETE), session binding, caching, redaction | Implemented and tested |
| mcp-auth integration (external credential connector, assertion exchange) | Implemented and tested, including against the real `auth-server` binary |
| GitLab backend mapping (`X-GitLab-Username`, `X-GitLab-PAT-Token`) | Implemented and tested with fakes |
| Gerrit and Atlassian mappings | **Configuration shapes only.** Expressible with the same model (see [backends](docs/backends.md)) but **not validated** against the real backends |
| Container image | `Dockerfile` provided; image build **not yet run in CI** |
| **Live validation with a real 1Password service account** | **Pending** (needs the service account; see [canary](docs/canary.md)) |

## How a request flows

```
MCP client --(1) POST /gitlab/mcp, no token--------------> plugin
          <-(2) 401 + WWW-Authenticate: resource_metadata
MCP client --(3) GET protected-resource metadata---------> plugin  -> names mcp-auth as the authorization server
MCP client --(4) OAuth authorization code + PKCE---------> mcp-auth   (token bound to the plugin's resource URL)
MCP client --(5) POST /gitlab/mcp + Bearer token---------> plugin
                 plugin: verify signature, issuer, audience, expiry, scopes
plugin     --(6) token exchange (private_key_jwt)--------> mcp-auth
                 mcp-auth: verify client, token, binding, ACTIVE SESSION -> signed assertion (no secrets)
                 plugin: verify assertion, look up (issuer, subject, service) mapping
plugin     --(7) read op:// references-------------------> 1Password (read-only service account)
plugin     --(8) forward + injected headers--------------> fixed upstream MCP server
```

Steps 5 to 8 run on **every** request, including GET/SSE and DELETE. A successful
`initialize` authorises nothing later. The exact wire contract is in
[`docs/external-credentials.md`](https://github.com/mcp-runtime/mcp-auth/blob/main/docs/external-credentials.md).

## Security properties

* Credential choice comes only from the verified `(issuer, subject)` and the service bound
  to the URL the request arrived on. Never from a header, parameter, vault path or session id.
* Client headers are copied from an **allowlist** (`Accept`, `Content-Type`,
  `Mcp-Protocol-Version`, `Last-Event-Id`, and the unwrapped `Mcp-Session-Id`). Caller
  credentials, cookies, `X-Forwarded-*`, identity headers, hop-by-hop headers and
  destination overrides are dropped. The upstream URL is fixed; query and path are ignored.
* Upstream redirects are not followed, so injected credentials cannot be sent elsewhere.
  An upstream `401` is reported as `502 upstream_credentials_rejected` and its
  `WWW-Authenticate` is not relayed.
* The client's mcp-auth bearer token is never forwarded upstream.
* Upstream session ids are returned as `<id>.<hmac>` bound to issuer, subject and service.
  Reuse by another user or against another service is rejected (`403`). It is stateless, so
  it survives restarts and works across replicas that share the key.
* Header values containing CR, LF, NUL or other control or non-ASCII characters are rejected.
* Errors, logs and `/healthz` `/readyz` carry no provider messages, references or
  credential values. The 1Password token's printed form is redacted.
* Expired cache entries are never served, including during a provider outage.

## Configuration

`ONEPASSWORD_MCP_CONFIG` (default `/etc/onepassword-mcp/config.json`). Unknown keys and
invalid values stop the process at startup with a clear error. Secrets are referenced by
mounted-file path, never inlined. See [`examples/config.production.json`](examples/config.production.json).

| Key | Meaning |
| --- | --- |
| `authorization_server.issuer` | mcp-auth issuer (https). `token_endpoint` and `jwks_uri` default from it. |
| `public_path_prefix` | Path prefix an ingress strips before the request reaches the plugin. The plugin serves the canonical path and the stripped one. |
| `session_binding_key_file` | At least 32 characters. Shared by all replicas. |
| `mappings_file` | Administrator-managed identity to reference mappings. Reloaded when it changes. |
| `onepassword.service_account_token_file` | Mounted read-only service-account token. |
| `credential_cache.ttl_seconds` / `max_entries` | Default `300` / `1024`. `0` disables caching. |
| `limits` | Request size cap, upstream connect timeout, response-header timeout. |
| `services[]` | `id`, canonical `resource_url`, fixed `upstream_url`, `required_scopes`, its own `resource_client`, and `headers`. |

Header rules (`headers`) use exactly three transforms:

| `type` | Result |
| --- | --- |
| `direct` | `Name: <field>` |
| `prefix` | `Name: Bearer <field>` (prefixes limited to `Bearer`, `Token`) |
| `basic` | `Name: Basic base64(<username_field>:<password_field>)` |

Credential fields are arbitrary names (`username`, `api_token`, `email`, `http_password`, ...).
Nothing is evaluated or templated. Headers the gateway owns (`Host`, `Cookie`,
`Content-*`, `Mcp-*`, `X-Forwarded-*`, ...) cannot be configured.

## Caching, rotation and revocation

* Every request is authorised by mcp-auth first. Credentials are cached only after that.
* Cache key: verified issuer, subject, service and mapping version. Bounded LRU, in memory.
* Entry lifetime is `min(credential_cache.ttl_seconds, remaining lifetime of the access token)`.
* **Maximum delay before a rotated secret is visible: `credential_cache.ttl_seconds`
  (default 5 minutes)**, less if the token expires sooner. Set `0` for immediate visibility
  at the cost of a 1Password read per request.
* Changing a reference in the mapping file changes the mapping version, so it takes effect on
  the next request. Removing a mapping denies the next request regardless of the cache.
* A revoked mcp-auth session is refused on the next request.
* If the mapping file becomes unreadable or invalid after start, the plugin fails closed (`503`).

## Run locally

See [`docs/deployment.md`](docs/deployment.md). Run the tests:

```bash
go test ./...            # unit + gateway tests with fakes (e2e skips without an auth-server)
go test -race ./...
# end-to-end against the real authorization server (a mcp-auth checkout with PR #24):
MCP_AUTH_SRC=/path/to/mcp-auth go test ./e2e/
```

## Documentation

* [Onboarding, rotation and offboarding](docs/onboarding.md)
* [Client configuration: before and after](docs/client-config.md)
* [Backends: GitLab, Gerrit, Atlassian](docs/backends.md)
* [Deployment](docs/deployment.md)
* [Live canary](docs/canary.md)
* [Wire contract](https://github.com/mcp-runtime/mcp-auth/blob/main/docs/external-credentials.md)
