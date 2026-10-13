# Architecture

## Components

```mermaid
flowchart LR
    subgraph clientside["User machine"]
        client["MCP client<br/>URL only, no credentials"]
    end

    subgraph platform["Your platform"]
        auth["mcp-auth<br/>OAuth authorization server<br/>login, consent, tokens, sessions"]
        idp["Identity provider<br/>SSO / OIDC"]
        subgraph plugin["onepassword-mcp-plugin"]
            gw["Gateway<br/>verify token, forward"]
            map["Mappings<br/>identity to op:// refs"]
            res["Resolver<br/>1Password SDK"]
        end
    end

    op[("1Password<br/>org vault")]
    up["Upstream MCP server<br/>GitLab, Gerrit, Atlassian"]

    client -- "1. OAuth (code + PKCE)" --> auth
    auth -- "login" --> idp
    client -- "2. MCP + Bearer token" --> gw
    gw -- "3. token exchange<br/>private_key_jwt" --> auth
    gw --> map --> res
    res -- "4. read-only service account" --> op
    gw -- "5. request + injected headers" --> up
```

| Component | Owns |
| --- | --- |
| **MCP client** | Standard MCP OAuth. Holds only a short-lived token. Never sees upstream credentials. |
| **mcp-auth** | Login, consent, tokens, session validity and revocation, resource and scope policy, authenticating the plugin, minting the credential-access assertion. No secret-manager dependency. |
| **Plugin gateway** | Verifying the token on every request, requesting the assertion, header allowlisting, injection, streaming, session binding. |
| **Mappings** | Administrator-approved `(issuer, subject, service)` to `op://` references. |
| **Resolver** | The only code that imports the 1Password SDK and holds the service-account token. |
| **1Password** | Where users store their own credentials, in an organization or integration vault. |

The plugin is **not** an authorization server. A 1Password service account authenticates the
plugin to 1Password. It says nothing about who the human user is; that comes only from mcp-auth.

## Request flow

```mermaid
sequenceDiagram
    autonumber
    actor C as MCP client
    participant P as Plugin
    participant A as mcp-auth
    participant V as 1Password
    participant U as Upstream MCP server

    C->>P: POST /gitlab/mcp (no token)
    P-->>C: 401 + WWW-Authenticate (resource_metadata)
    C->>P: GET protected-resource metadata
    P-->>C: authorization server = mcp-auth
    C->>A: OAuth authorization code + PKCE (resource-bound)
    A-->>C: access token (aud = plugin URL, sid = session)

    loop every request: POST, GET/SSE, DELETE
        C->>P: request + Bearer token
        P->>P: verify signature, issuer, audience, expiry, scopes
        P->>A: token exchange (private_key_jwt)
        A->>A: verify client, token, binding, ACTIVE session
        A-->>P: signed assertion (no credentials)
        P->>P: look up mapping for (issuer, subject, service)
        opt cache miss
            P->>V: resolve op:// references
            V-->>P: field values
        end
        P->>U: request + injected headers (fixed URL)
        U-->>P: response (streamed)
        P-->>C: response (streamed)
    end
```

Every step after the token arrives runs on **every** request. A successful `initialize`
authorises nothing later. The exact exchange contract is in
[mcp-auth's wire contract](https://github.com/mcp-runtime/mcp-auth/blob/main/docs/external-credentials.md).

## Trust boundaries

* mcp-auth holds only the plugin's **public** key and connector settings. It has no 1Password
  SDK and no service-account token.
* The plugin holds the service-account token and the resource-client private keys, from mounted
  secret files. They are not in the image.
* Raw credentials exist only inside the plugin and on the connection to the upstream.

