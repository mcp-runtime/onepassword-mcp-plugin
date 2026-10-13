# Backends

The gateway is generic. A backend is a `services[]` entry: a fixed `upstream_url`, the
header rules, and its own mcp-auth resource client. Adding a service does not add a
credential-resolution path.

## GitLab (first implementation)

Implemented and tested with fakes. Live validation against a real GitLab MCP backend with a
real service account is **pending**.

```json
"headers": [
  { "name": "X-GitLab-Username",  "type": "direct", "field": "username" },
  { "name": "X-GitLab-PAT-Token", "type": "direct", "field": "api_token" }
]
```

## Gerrit and Atlassian: configuration shapes only

The Gerrit and Atlassian MCP backends were **not inspected or exercised** in this change.
The shapes below show that the model can express the usual schemes. **Confirm each
backend's real header requirements before enabling it.**

| Backend | Commonly used scheme | Shape |
| --- | --- | --- |
| Gerrit | HTTP Basic: account username with the generated HTTP password | `basic`, `username_field` + `password_field` |
| Atlassian | HTTP Basic: account email with an API token (some endpoints accept a Bearer API key) | `basic`, or `prefix` with `Bearer` |

```json
{ "name": "Authorization", "type": "basic", "username_field": "username", "password_field": "http_password" }
{ "name": "Authorization", "type": "basic", "username_field": "email", "password_field": "api_token" }
{ "name": "Authorization", "type": "prefix", "prefix": "Bearer", "field": "api_token" }
```

## Adding another service

1. Decide the credential fields and header rules.
2. Register a resource client and an external credential connector for its URL in mcp-auth
   (`MCP_AUTH_EXTERNAL_CREDENTIALS_FILE`) and allow its scopes.
3. Add a `services[]` entry and mappings for each user.
