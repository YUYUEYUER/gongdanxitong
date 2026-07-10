package migrations

import (
	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V2_5_2 adds one-time customer registration verification, stable OIDC
// identities, and a per-user session version for global session revocation.
func V2_5_2(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS security_migration_markers (
			key TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
		);

		ALTER TABLE users
			ADD COLUMN IF NOT EXISTS session_version BIGINT DEFAULT 1 NOT NULL,
			ADD COLUMN IF NOT EXISTS portal_registered BOOLEAN DEFAULT FALSE NOT NULL;
		ALTER TABLE inboxes
			ADD COLUMN IF NOT EXISTS widget_session_version BIGINT DEFAULT 1 NOT NULL;

		-- Move authentication state out of administrator and Widget-controlled
		-- custom attributes. OR keeps this statement idempotent for partial runs.
		UPDATE users
		SET portal_registered = portal_registered OR
				(lower(COALESCE(custom_attributes->>'portal_registered', 'false')) = 'true'),
			custom_attributes = COALESCE(custom_attributes, '{}'::jsonb) - 'portal_registered'
		WHERE type IN ('contact', 'visitor');

		-- Credential invalidation is deliberately guarded by a durable marker.
		-- If the schema commit succeeds but recording the app version fails, a
		-- retry must not revoke newly recovered credentials a second time.
		DO $security_reset$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM security_migration_markers
				WHERE key = 'v2.5.2-legacy-identity-reset'
			) THEN
				-- The legacy registration flow could assign a password without
				-- proving mailbox ownership.
				UPDATE users
				SET password = NULL,
					session_version = session_version + 1,
					reset_password_token = NULL,
					reset_password_token_expiry = NULL,
					api_key = NULL,
					api_secret = NULL,
					api_key_last_used_at = NULL,
					updated_at = NOW()
				WHERE type = 'contact'
				  AND deleted_at IS NULL
				  AND portal_registered = true;

				-- Legacy agent sessions and API keys cannot be attributed to a
				-- stable OIDC subject safely.
				UPDATE users
				SET session_version = session_version + 1,
					api_key = NULL,
					api_secret = NULL,
					api_key_last_used_at = NULL,
					updated_at = NOW()
				WHERE type = 'agent'
				  AND deleted_at IS NULL;

				INSERT INTO security_migration_markers(key)
				VALUES ('v2.5.2-legacy-identity-reset');
			END IF;
		END
		$security_reset$;

		CREATE TABLE IF NOT EXISTS customer_portal_registrations (
			id BIGSERIAL PRIMARY KEY,
			created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
			updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
			email TEXT NOT NULL UNIQUE,
			first_name TEXT NOT NULL,
			last_name TEXT NOT NULL DEFAULT '',
			token_hash CHAR(64) NOT NULL UNIQUE,
			expires_at TIMESTAMPTZ NOT NULL,
			CONSTRAINT constraint_customer_portal_registrations_email CHECK (LENGTH(email) <= 320),
			CONSTRAINT constraint_customer_portal_registrations_first_name CHECK (LENGTH(first_name) <= 140),
			CONSTRAINT constraint_customer_portal_registrations_last_name CHECK (LENGTH(last_name) <= 140)
		);
		CREATE INDEX IF NOT EXISTS index_customer_portal_registrations_expires_at
			ON customer_portal_registrations(expires_at);
		ALTER TABLE customer_portal_registrations
			DROP COLUMN IF EXISTS password_hash;

		CREATE TABLE IF NOT EXISTS oidc_user_identities (
			id BIGSERIAL PRIMARY KEY,
			created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
			last_seen_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
			provider_id INT REFERENCES oidc(id) ON DELETE CASCADE ON UPDATE CASCADE NOT NULL,
			issuer TEXT NOT NULL,
			subject TEXT NOT NULL,
			user_id BIGINT REFERENCES users(id) ON DELETE CASCADE ON UPDATE CASCADE NOT NULL,
			email_at_link TEXT NOT NULL,
			CONSTRAINT constraint_oidc_identity_issuer CHECK (LENGTH(issuer) <= 2048),
			CONSTRAINT constraint_oidc_identity_subject CHECK (LENGTH(subject) <= 512),
			CONSTRAINT constraint_oidc_identity_email CHECK (LENGTH(email_at_link) <= 320),
			UNIQUE (issuer, subject),
			UNIQUE (provider_id, user_id)
		);
		CREATE INDEX IF NOT EXISTS index_oidc_user_identities_user_id
			ON oidc_user_identities(user_id);

		-- Recover ownership for historical message attachments where it can be
		-- derived unambiguously. Remaining NULL owners still count against the
		-- instance-wide media quota.
		UPDATE media m
		SET owner_user_id = cm.sender_id
		FROM conversation_messages cm
		WHERE m.owner_user_id IS NULL
		  AND m.model_type = 'messages'
		  AND m.model_id = cm.id
		  AND cm.sender_id IS NOT NULL;

		UPDATE media m
		SET owner_user_id = u.id
		FROM users u
		WHERE m.owner_user_id IS NULL
		  AND m.model_type = 'users'
		  AND m.model_id = u.id;

		-- A conversation has exactly one public survey bearer. Preserve a real
		-- submitted response over an unanswered duplicate before enforcing that
		-- invariant in the database.
		WITH ranked AS (
			SELECT id,
				ROW_NUMBER() OVER (
					PARTITION BY conversation_id
					ORDER BY (response_timestamp IS NOT NULL) DESC,
						response_timestamp DESC NULLS LAST,
						id DESC
				) AS row_number
			FROM csat_responses
		)
		DELETE FROM csat_responses
		WHERE id IN (SELECT id FROM ranked WHERE row_number > 1);
		DROP INDEX IF EXISTS index_csat_responses_on_conversation_id;
		CREATE UNIQUE INDEX IF NOT EXISTS index_unique_csat_responses_on_conversation_id
			ON csat_responses(conversation_id);

		UPDATE roles
		SET permissions = array_append(permissions, 'messages:write_as_contact')
		WHERE name = 'Admin'
		  AND NOT ('messages:write_as_contact' = ANY(permissions));

		-- Tighten only the legacy defaults. Administrators can explicitly grant
		-- broader file types or global conversation access again after upgrade.
		UPDATE settings
		SET value = '["jpg","jpeg","png","gif","pdf","txt","csv","doc","docx","xls","xlsx","ppt","pptx"]'::jsonb,
			updated_at = NOW()
		WHERE key = 'app.allowed_file_upload_extensions'
		  AND value = '["*"]'::jsonb;

		UPDATE settings
		SET value = value - 'webp', updated_at = NOW()
		WHERE key = 'app.allowed_file_upload_extensions'
		  AND jsonb_typeof(value) = 'array'
		  AND value ? 'webp';

		UPDATE roles
		SET permissions = array_remove(permissions, 'conversations:read_all')
		WHERE name = 'Agent'
		  AND 'conversations:read_all' = ANY(permissions);
	`); err != nil {
		return err
	}
	return tx.Commit()
}
