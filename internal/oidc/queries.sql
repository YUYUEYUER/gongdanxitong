-- name: get-all-oidc
SELECT id, created_at, updated_at, name, provider_url, client_id, client_secret, enabled, provider, logo_url FROM oidc ORDER BY updated_at DESC;

-- name: get-oidc
SELECT id, created_at, updated_at, name, provider_url, client_id, client_secret, enabled, provider, logo_url FROM oidc WHERE id = $1;

-- name: insert-oidc
INSERT INTO oidc (name, provider, provider_url, client_id, client_secret, logo_url)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: update-oidc
UPDATE oidc
SET name = $2, provider = $3, provider_url = $4, client_id = $5, client_secret = $6, enabled = $7, logo_url = $8, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: delete-oidc
DELETE FROM oidc WHERE id = $1;

-- name: lock-oidc-for-delete
SELECT id FROM oidc WHERE id = $1 FOR UPDATE;

-- name: get-oidc-identity-user-ids
SELECT user_id
FROM oidc_user_identities
WHERE provider_id = $1
FOR UPDATE;

-- name: revoke-oidc-identity-sessions
UPDATE users
SET session_version = session_version + 1,
	api_key = NULL,
	api_secret = NULL,
	api_key_last_used_at = NULL,
    updated_at = now()
WHERE id = ANY($1::bigint[]);

-- name: resolve-oidc-user-identity
UPDATE oidc_user_identities
SET last_seen_at = now()
WHERE issuer = $1 AND subject = $2
RETURNING user_id;

-- name: bind-oidc-user-identity
INSERT INTO oidc_user_identities (provider_id, issuer, subject, user_id, email_at_link)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (issuer, subject) DO UPDATE SET last_seen_at = now()
RETURNING user_id;
