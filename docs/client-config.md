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
