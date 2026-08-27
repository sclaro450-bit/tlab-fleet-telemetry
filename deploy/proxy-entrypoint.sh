#!/bin/sh
set -eu

configuration_error() {
  echo "CONFIGURATION ERROR: $1" >&2
  exit 2
}

: "${PROXY_DOMAIN:?CONFIGURATION ERROR: missing PROXY_DOMAIN}"
: "${ACME_EMAIL:?CONFIGURATION ERROR: missing ACME_EMAIL}"
: "${CLOUDFLARE_API_TOKEN:?CONFIGURATION ERROR: missing CLOUDFLARE_API_TOKEN}"

# lego's Cloudflare provider uses this variable name for scoped API tokens.
export CF_DNS_API_TOKEN="$CLOUDFLARE_API_TOKEN"

if [ -n "${TESLA_PRIVATE_KEY_B64:-}" ]; then
  printf '%s' "$TESLA_PRIVATE_KEY_B64" | base64 -d > "$TESLA_KEY_FILE" \
    || configuration_error "TESLA_PRIVATE_KEY_B64 is not valid base64"
elif [ -n "${TESLA_PRIVATE_KEY:-}" ]; then
  printf '%s\n' "$TESLA_PRIVATE_KEY" > "$TESLA_KEY_FILE"
else
  configuration_error "missing TESLA_PRIVATE_KEY_B64"
fi

chmod 0600 "$TESLA_KEY_FILE"
openssl ec -in "$TESLA_KEY_FILE" -check -noout >/dev/null 2>&1 \
  || configuration_error "the Tesla private key is not a valid EC private key"

if [ -n "${TESLA_PUBLIC_KEY_URL:-}" ]; then
  remote_key=/run/tesla/registered-public-key.pem
  derived_key=/run/tesla/derived-public-key.pem
  curl --fail --silent --show-error --max-time 15 "$TESLA_PUBLIC_KEY_URL" -o "$remote_key" \
    || configuration_error "cannot download TESLA_PUBLIC_KEY_URL"
  openssl ec -in "$TESLA_KEY_FILE" -pubout -out "$derived_key" >/dev/null 2>&1
  remote_fingerprint=$(openssl pkey -pubin -in "$remote_key" -outform DER 2>/dev/null | sha256sum | cut -d' ' -f1)
  derived_fingerprint=$(openssl pkey -pubin -in "$derived_key" -outform DER 2>/dev/null | sha256sum | cut -d' ' -f1)
  [ "$remote_fingerprint" = "$derived_fingerprint" ] \
    || configuration_error "private key does not match the public key registered on the application domain"
fi

mkdir -p "$ACME_STORAGE"
cert_file="$ACME_STORAGE/certificates/$PROXY_DOMAIN.crt"
cert_key="$ACME_STORAGE/certificates/$PROXY_DOMAIN.key"

if [ -s "$cert_file" ] && [ -s "$cert_key" ]; then
  echo "Checking ACME certificate renewal for $PROXY_DOMAIN"
  lego --accept-tos --email "$ACME_EMAIL" --dns cloudflare \
    --domains "$PROXY_DOMAIN" --path "$ACME_STORAGE" renew --days 30
else
  echo "Obtaining ACME certificate for $PROXY_DOMAIN"
  lego --accept-tos --email "$ACME_EMAIL" --dns cloudflare \
    --domains "$PROXY_DOMAIN" --path "$ACME_STORAGE" run
fi

[ -s "$cert_file" ] || configuration_error "ACME certificate was not created"
[ -s "$cert_key" ] || configuration_error "ACME certificate key was not created"

openssl x509 -in "$cert_file" -noout >/dev/null 2>&1 \
  || configuration_error "ACME certificate is not valid PEM"
cert_public_fingerprint=$(openssl x509 -in "$cert_file" -pubkey -noout 2>/dev/null | openssl pkey -pubin -outform DER 2>/dev/null | sha256sum | cut -d' ' -f1)
key_public_fingerprint=$(openssl pkey -in "$cert_key" -pubout -outform DER 2>/dev/null | sha256sum | cut -d' ' -f1)
[ "$cert_public_fingerprint" = "$key_public_fingerprint" ] \
  || configuration_error "ACME certificate and TLS private key do not match"

export TESLA_HTTP_PROXY_TLS_CERT="$cert_file"
export TESLA_HTTP_PROXY_TLS_KEY="$cert_key"

chown -R tesla:tesla /run/tesla "$ACME_STORAGE"

echo "Starting Tesla Vehicle Command Proxy on 0.0.0.0:${TESLA_HTTP_PROXY_PORT}"
set +e
su-exec tesla:tesla /usr/local/bin/tesla-http-proxy \
  -key-file "$TESLA_KEY_FILE" \
  -cert "$cert_file" \
  -tls-key "$cert_key" \
  -host 0.0.0.0 \
  -port "$TESLA_HTTP_PROXY_PORT" \
  -timeout "${TESLA_HTTP_PROXY_TIMEOUT:-15s}" \
  -verbose
proxy_status=$?
set -e

echo "FATAL: Tesla Vehicle Command Proxy exited with status ${proxy_status}" >&2
exit "$proxy_status"
