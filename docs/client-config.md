# Client configuration: before and after

## Before: credentials in every client's `mcp.json`

```json
{
  "mcpServers": {
    "gitlab": {
      "url": "https://mcp.example.com/gitlab/mcp",
      "headers": {
        "X-GitLab-Username": "alice",
        "X-GitLab-PAT-Token": "glpat-xxxxxxxxxxxxxxxx"
      }
    }
  }
}
```

Every user keeps a long-lived token in a plaintext file on every machine and every client.

## After: URL only

```json
{
  "mcpServers": {
    "gitlab": {
      "url": "https://mcp.example.com/mcp-proxy/gitlab/mcp"
    }
  }
}
```

* No `X-GitLab-Username`, no `X-GitLab-PAT-Token`, no `Authorization` header.
* On first use the client discovers mcp-auth from the plugin, opens the browser sign-in
  and consent page, and stores only the short-lived OAuth token. Clients that support the
  MCP authorization flow (RFC 9728 discovery, PKCE, resource indicators) need nothing more.
* The URL is the plugin's canonical resource URL. Use the exact value configured as the
  service's `resource_url`.
* Remove any static credential headers from existing configs. The plugin ignores and
  drops client-supplied copies, but leaving them in place keeps the secrets on disk.

## Connect each client (mcp-auth v0.5.0)

All clients get the same single URL: the service's `resource_url`. Nothing secret goes in
the client config.

| Client | Config |
| --- | --- |
| Cursor (`~/.cursor/mcp.json` or `.cursor/mcp.json`) | `{"mcpServers": {"gitlab": {"url": "<resource_url>"}}}` |
| Claude Code | `claude mcp add --transport http gitlab <resource_url>`, then `/mcp` to sign in |
| Codex (`~/.codex/config.toml`) | `[mcp_servers.gitlab]` with `url = "<resource_url>"`, then `codex mcp login gitlab` |
| Cursor CLI | the same `mcp.json`, then `agent mcp login gitlab` |

Check each client's own documentation for the exact current syntax; the only plugin-specific
part is the URL.

### What the authorization server must provide

* Dynamic client registration (`MCP_AUTH_REGISTRATION_ENABLED=true`) or client ID metadata
  documents (on by default), because these clients register themselves.
* The service's `resource_url` in `MCP_AUTH_RESOURCES`, and its scopes in
  `MCP_AUTH_RESOURCE_SCOPES` (for example `{"<resource_url>": ["gitlab:use"]}`).
* An entry in `MCP_AUTH_EXTERNAL_CREDENTIALS_FILE` whose `required_scopes` are a subset of
  those scopes.

Since mcp-auth v0.5.0 a client that omits `scope` on the authorize request (Cursor CLI does)
is granted the resource's full scope list, so the plugin's `required_scopes` are satisfied
without any client-side scope setting. Older mcp-auth versions minted an empty scope here and
the plugin rejected the token.

### Checking it works

1. The client's first request receives `401` with a `WWW-Authenticate` header that names the
   protected-resource metadata.
2. The client signs in once in the browser and stores only the short-lived OAuth token.
3. Revoke the login (or sign out in mcp-auth): the next request fails with `401`, even before
   the access token expires, because the plugin asks mcp-auth for a fresh assertion that
   requires an active session.
