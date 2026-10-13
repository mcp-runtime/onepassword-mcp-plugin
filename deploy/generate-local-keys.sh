#!/usr/bin/env bash
# Generates throwaway keys for a LOCAL trial only. Output goes to ./secrets (git-ignored).
# Never reuse these for a real deployment, and never put the 1Password token in this directory
# until you are deliberately running the canary.
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p secrets
umask 077
[ -f secrets/as-signing.pem ] || openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out secrets/as-signing.pem
[ -f secrets/gitlab-plugin.pem ] || openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out secrets/gitlab-plugin.pem
openssl pkey -in secrets/gitlab-plugin.pem -pubout -out secrets/gitlab-plugin.pub.pem
[ -f secrets/session-binding.key ] || openssl rand -base64 48 | tr -d '\n' > secrets/session-binding.key
[ -f secrets/op-service-account.token ] || printf 'REPLACE-WITH-READ-ONLY-SERVICE-ACCOUNT-TOKEN' > secrets/op-service-account.token
chmod 600 secrets/*
echo "keys written to $(pwd)/secrets"
