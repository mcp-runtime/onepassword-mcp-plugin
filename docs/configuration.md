# Configuration

`ONEPASSWORD_MCP_CONFIG` (default `/etc/onepassword-mcp/config.json`). Unknown keys and
invalid values stop the process at startup with a clear error. Secrets are referenced by
mounted-file path, never inlined. See [`examples/config.production.json`](../examples/config.production.json).

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
