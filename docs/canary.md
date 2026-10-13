# Live canary (pending)

Automated tests use fake credentials and a fake resolver. **Real 1Password access has not
been exercised.** Run this once a read-only service account is available.

## Preconditions

* A 1Password organization or integration vault (not Personal, Private, Employee or the
  default Shared vault) containing one test item per canary user.
* A read-only service account granted that vault only, and its token in a mounted file.
* mcp-auth with login configured and the external credential connector set up ([connect guide](connect-mcp-auth.md)).
* A test GitLab backend and two test users with distinct tokens.

## Checks

1. Plugin starts. `/readyz` is 200.
2. User A signs in from a real MCP client with a URL-only config; a GitLab tool call succeeds
   and the backend log shows user A's identity.
3. User B does the same and the backend shows user B's identity, not A's.
4. Rotate user A's token in 1Password. After `credential_cache.ttl_seconds` the new value is
   used. The old token no longer works upstream.
5. Revoke user A's mcp-auth session. The next call fails with `401` and 1Password access
   logs show no read for it.
6. Remove user A's mapping. The next call fails with `403 no_credential_mapping`.
7. Stop access to 1Password (revoke the service account). Calls return
   `503 credential_provider_unavailable`, with no cached value served after expiry.
8. Grep the plugin and mcp-auth logs for token and credential values: none.

Record results in the ticket. Until then, live validation is **pending**, not complete.
