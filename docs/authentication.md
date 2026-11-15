# Authentication

Every route except `GET /health` requires `Authorization: Bearer <JWT>`, and
this service verifies the token itself (ADR-006): the gateway only checks
that a credential is present, so it does not replace this validation.

- **Algorithm:** only `RS256`. `none`, `HS256` and any other algorithm are
  rejected whatever the token header says.
- **Key:** the public key of `synkro-auth-api`, as PEM in `JWT_PUBLIC_KEY`
  (newlines may be written as `\n`) or as a file path in
  `JWT_PUBLIC_KEY_FILE` - exactly one of them. RSA, at least 2048 bits. A
  private key is refused. The service does not start without a valid key.
- **Claims:** `exp` is required and `sub`, `iat`, `jti`, `roles` and
  `permissions` must be present. Clock skew of 30 s is tolerated on `exp`,
  `nbf` and `iat`. A person's `sub` is a UUID; a `SERVICE` token's `sub` is the
  service name.
- **Failures:** a missing, malformed, expired, wrongly signed or non-RS256
  token is `401 UNAUTHORIZED` with one generic message, so the response does
  not say which check failed. A valid token without the role or permission is
  `403 FORBIDDEN`.
- Identity, roles and permissions come only from the verified claims; any
  `X-User-*` header is ignored.
