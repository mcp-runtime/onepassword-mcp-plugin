# Security properties

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
