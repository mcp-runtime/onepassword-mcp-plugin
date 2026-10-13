#!/usr/bin/env bash
# Creates the key pair the plugin uses to authenticate to mcp-auth for one service.
#   deploy/generate-resource-client.sh gitlab
# Private key  -> secrets/<service>-plugin.pem      (mount into the plugin only)
# Public key   -> secrets/<service>-plugin.pub.pem  (give to mcp-auth)
set -euo pipefail
service="${1:?usage: $0 <service-id>}"
cd "$(dirname "$0")"
mkdir -p secrets
umask 077
[ -f "secrets/${service}-plugin.pem" ] || openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "secrets/${service}-plugin.pem"
openssl pkey -in "secrets/${service}-plugin.pem" -pubout -out "secrets/${service}-plugin.pub.pem"
chmod 600 "secrets/${service}-plugin.pem"
echo "private: $(pwd)/secrets/${service}-plugin.pem  (plugin only)"
echo "public:  $(pwd)/secrets/${service}-plugin.pub.pem  (mcp-auth)"
