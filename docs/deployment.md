# Deployment

## Artifacts

* Module: `github.com/mcp-runtime/onepassword-mcp-plugin` (own `go.mod`).
* Image: build from the repository root:
  `docker build --build-arg VERSION=0.1.0 -t onepassword-mcp-plugin .`
  Static, non-root, distroless. Contains **no** token, mapping or key.
* Release tags: `vX.Y.Z` (this repo). Published to `ghcr.io/mcp-runtime/onepassword-mcp-plugin` by the release workflow, independent of mcp-auth's tags.

## Runtime inputs (all mounted, none baked in)

| Mount | Contents | Secret |
| --- | --- | --- |
| `/etc/onepassword-mcp/config.json` | Configuration | No |
| `/etc/onepassword-mcp/mappings.json` | Identity to reference mappings (access policy) | Treat as sensitive |
| `/run/secrets/op-service-account.token` | Read-only 1Password service-account token | **Yes** |
| `/run/secrets/<service>-plugin.pem` | Resource-client private key, one per service | **Yes** |
| `/run/secrets/session-binding.key` | HMAC key for session binding, at least 32 chars | **Yes** |

mcp-auth receives only the resource clients' **public** keys and the connector file. It needs
no 1Password SDK and no service-account token.

## mcp-auth side

For each service, add to `MCP_AUTH_EXTERNAL_CREDENTIALS_FILE` (see
[wire contract](https://github.com/mcp-runtime/mcp-auth/blob/main/docs/external-credentials.md)):

```json
[{ "id": "gitlab-1password", "resource": "https://mcp.example.com/mcp-proxy/gitlab/mcp",
   "required_scopes": ["gitlab:use"],
   "resource_client": { "client_id": "gitlab-1password-plugin", "public_key_file": "/etc/mcp-auth/gitlab-plugin.pub.pem" } }]
```

and list the resource URL in `MCP_AUTH_RESOURCES` with its scopes in `MCP_AUTH_RESOURCE_SCOPES`.
Use a real login connector (`MCP_AUTH_CONNECTOR`). Identity and login selection stay
mcp-auth policy.

## Issuer and token endpoint

The plugin checks the token's `iss` against `authorization_server.issuer` and signs its
client assertion for the token endpoint URL, which mcp-auth expects to equal
`<issuer>/token`. The plugin therefore has to reach mcp-auth at its **issuer URL**. If the
issuer's public name is not routable from the plugin, make it resolvable (internal DNS or a
private route to the same server) rather than pointing `token_endpoint` at a different host.

## Ingress and path prefixes

The `resource_url` must be the exact public URL clients use, because it is the token
audience. If the ingress strips a prefix, set `public_path_prefix` to that prefix. The
plugin serves both the canonical and stripped paths and the matching well-known metadata
paths. Terminate TLS in front of the plugin. Do not expose the plugin's port without TLS.
Disable response buffering on the ingress for the MCP paths so SSE streams promptly.

## Replicas

Stateless apart from caches. Replicas must share `session-binding.key` and the mapping
file. The credential cache and the assertion replay set are per process: the replay set is
a defence in depth, since mcp-auth already refuses a replayed client assertion.

## Health

`GET /healthz` is liveness. `GET /readyz` reports readiness. Neither calls 1Password or
mcp-auth, and neither returns secrets.

## Upgrades

Independent of mcp-auth: restart or roll the plugin without restarting mcp-auth, and the
reverse. The only coupling is the exchange contract. mcp-auth must be a version that
supports external credential connectors and issues `sid`-bearing tokens (users signed in
before that upgrade must sign in again).

## Local trial

```bash
deploy/generate-local-keys.sh
echo '127.0.0.1 mcp-auth' | sudo tee -a /etc/hosts       # issuer host must resolve for browser and plugin
cp deploy/mappings.json.example deploy/mappings.json   # set issuer to http://mcp-auth:8080 and subject to "alice"
docker compose -f deploy/docker-compose.yml up --build
```

This runs mcp-auth in its insecure local-development mode (fixed login subject, plain
HTTP). It exercises discovery and OAuth. Credential retrieval needs a real service account:
see [canary](canary.md).
