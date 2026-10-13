# Onboarding, rotation and offboarding

Administrator-managed enrollment is implemented. The mapping source sits behind the
`mappings.Store` interface so authenticated self-service enrollment can be added later
without touching the gateway.

## Vaults: read this first

A 1Password service account can only read vaults it has been explicitly granted. It
**cannot** access built-in Personal, Private, Employee or the default Shared vaults.
Create an organization or integration vault (for example `MCP-Credentials`), grant the
service account **read-only** access to it, and keep MCP credentials there. Do not promise
users that anything in their personal vault will be picked up.

## Where users store credentials

One item per user per service, in the integration vault. Field names are yours to choose,
as long as they match the service's `headers` configuration.

| Service | Item name (suggested) | Required fields | Notes |
| --- | --- | --- | --- |
| GitLab | `gitlab-<user>` | `username`, `api_token` | Personal access token with the scopes the MCP server needs (typically `api` or `read_api`). |
| Gerrit | `gerrit-<user>` | `username`, `http_password` | The generated HTTP password, not the SSO password. |
| Atlassian | `atlassian-<user>` | `email`, `api_token` | Atlassian API token, with the account email. |

How the item gets into the vault is an organizational choice: users add their own
entries in a vault they are allowed to write to while the service account only reads, or an
administrator creates the item on their behalf. The plugin never writes to 1Password.

## Registering the mapping (administrator)

A mapping binds a **verified identity** to **complete field references**. Identity is the
`(issuer, subject)` pair from the mcp-auth access token. Use the issuer so the same subject
at a different issuer can never collide.

1. Find the user's `subject`. It is the `sub` claim mcp-auth issues, which is the identity
   provider claim chosen by that server's `identity_claims` setting (for example an email).
   Decode a token the user obtained, or check mcp-auth's audit log.
2. Add an entry to the mappings file (see `examples/mappings.example.json`):

   ```json
   {
     "issuer": "https://auth.example.com",
     "subject": "alice@example.com",
     "service": "gitlab",
     "fields": {
       "username": "op://MCP-Credentials/gitlab-alice/username",
       "api_token": "op://MCP-Credentials/gitlab-alice/api_token"
     }
   }
   ```

   References must be complete `op://vault/item/field` (or `vault/item/section/field`).
   Partial paths are rejected. Every field the service's headers use must be present.
3. Deploy the file. It is mounted read-only and re-read when it changes. A malformed file
   makes the plugin answer `503 mapping_unavailable` rather than serve stale access.
4. Ask the user to add the MCP URL to their client and sign in (see
   [client config](client-config.md)). They never handle the token.

Keep the real mappings file out of source control. Treat it as access policy: review changes.

## Rotating a credential (user)

1. Create the new token in the upstream service.
2. Update the **same field** in the same 1Password item.
3. Nothing else is needed. The new value is visible within `credential_cache.ttl_seconds`
   (default 5 minutes), or immediately if the cache is disabled or the plugin restarts.
4. Revoke the old token upstream after that window.

If the item or field is moved, an administrator updates the reference in the mapping file.
That takes effect on the next request.

## Offboarding or removing access (administrator)

Do both, in this order of effect:

1. **Remove the mapping entry** and deploy the file. The next request from that user for
   that service is denied (`403 no_credential_mapping`), cache included.
2. **Revoke the user's mcp-auth session** (and disable their identity at the identity
   provider). The next request is denied (`401`) before any vault access.
3. Revoke the upstream token and archive the 1Password item.

## Troubleshooting

| Response | Meaning |
| --- | --- |
| `401 invalid_token` | Token invalid, expired, or the mcp-auth session was revoked. The client should re-authorise. |
| `403 insufficient_scope` | Token lacks the service's required scope. |
| `403 no_credential_mapping` | No mapping for this issuer, subject and service. Ask an administrator. |
| `403 session_binding_mismatch` | A session id from another user or service was presented. |
| `502 credential_unavailable` | The mapped item or field could not be read (wrong reference, no access, empty). |
| `502 upstream_credentials_rejected` | The upstream refused the stored credential. Rotate it. |
| `503 credential_provider_unavailable` | 1Password was unreachable or timed out. |
| `503 authorization_unavailable` | mcp-auth was unreachable, or rejected this plugin's own resource client. |
