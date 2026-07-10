-- name: insert-media
INSERT INTO media (store, filename, content_type, size, meta, model_id, model_type, disposition, content_id, uuid, owner_user_id)
VALUES(
  $1, 
  $2, 
  $3, 
  $4, 
  $5, 
  NULLIF($6, 0),
  NULLIF($7, ''),
  $8,
  $9,
  $10,
  NULLIF($11, 0)
)
RETURNING id;

-- name: get-media
SELECT id, created_at, updated_at, "uuid", store, filename, content_type, content_id, owner_user_id, model_id, model_type, disposition, "size", meta
FROM media
WHERE
   ($1 > 0 AND id = $1)
   OR
   ($2 != '' AND uuid = NULLIF($2, '')::uuid)

-- name: get-media-by-uuid
SELECT id, created_at, updated_at, "uuid", store, filename, content_type, content_id, owner_user_id, model_id, model_type, disposition, "size", meta
FROM media
WHERE uuid = $1;

-- name: delete-media
DELETE FROM media
WHERE uuid = $1;

-- name: attach-to-model
UPDATE media
SET model_type = $2,
    model_id = $3
WHERE id = $1;

-- name: get-model-media
SELECT id, created_at, updated_at, "uuid", store, filename, content_type, content_id, owner_user_id, model_id, model_type, disposition, "size", meta
FROM media
WHERE model_type = $1
    AND model_id = $2;

-- name: get-unlinked-media
SELECT id, created_at, updated_at, "uuid", store, filename, content_type, content_id, owner_user_id, model_id, model_type, disposition, "size", meta
FROM media
WHERE (model_id IS NULL OR model_id = 0)
  AND created_at < NOW() - INTERVAL '1 day';

-- name: get-unlinked-media-usage
SELECT COUNT(*), COALESCE(SUM(
  COALESCE(size, 0) +
  CASE
    WHEN meta ? 'thumbnail_size' AND (meta->>'thumbnail_size') ~ '^[0-9]{1,10}$'
      THEN (meta->>'thumbnail_size')::bigint
    WHEN content_type LIKE 'image/%' THEN 131072
    ELSE 0
  END
), 0)
FROM media
WHERE owner_user_id = $1
  AND (model_id IS NULL OR model_id = 0);

-- name: get-owned-media-usage
SELECT COUNT(*), COALESCE(SUM(
  COALESCE(size, 0) +
  CASE
    WHEN meta ? 'thumbnail_size' AND (meta->>'thumbnail_size') ~ '^[0-9]{1,10}$'
      THEN (meta->>'thumbnail_size')::bigint
    WHEN content_type LIKE 'image/%' THEN 131072
    ELSE 0
  END
), 0)
FROM media
WHERE owner_user_id = $1;

-- name: lock-global-media-quota
SELECT pg_advisory_xact_lock(128026298704193);

-- name: get-global-media-usage
SELECT COALESCE(SUM(
  1 + CASE
    WHEN meta ? 'thumbnail_size' AND (meta->>'thumbnail_size') ~ '^[0-9]{1,10}$'
      THEN CASE WHEN (meta->>'thumbnail_size')::bigint > 0 THEN 1 ELSE 0 END
    WHEN content_type LIKE 'image/%' THEN 1
    ELSE 0
  END
), 0), COALESCE(SUM(
  COALESCE(size, 0) +
  CASE
    WHEN meta ? 'thumbnail_size' AND (meta->>'thumbnail_size') ~ '^[0-9]{1,10}$'
      THEN (meta->>'thumbnail_size')::bigint
    WHEN content_type LIKE 'image/%' THEN 131072
    ELSE 0
  END
), 0)
FROM media;

-- name: lock-media-owner
SELECT id
FROM users
WHERE id = $1 AND deleted_at IS NULL
FOR UPDATE;

-- name: content-id-exists
SELECT m.uuid
FROM media m
INNER JOIN conversation_messages cm ON cm.id = m.model_id
WHERE m.model_type = 'messages'
  AND m.content_id = $1
  AND cm.conversation_id = (SELECT id FROM conversations WHERE uuid = $2::uuid LIMIT 1);

-- name: get-media-by-content-ids
SELECT m.id, m.created_at, m.updated_at, m."uuid", m.store, m.filename, m.content_type, m.content_id, m.owner_user_id, m.model_id, m.model_type, m.disposition, m."size", m.meta
FROM media m
INNER JOIN conversation_messages cm ON cm.id = m.model_id
WHERE m.model_type = 'messages'
  AND m.content_id = ANY($1)
  AND cm.conversation_id = (SELECT id FROM conversations WHERE uuid = $2::uuid LIMIT 1);

-- name: set-media-content-id
UPDATE media
SET content_id = $2
WHERE id = $1
  AND (content_id IS NULL OR content_id = '');

-- name: set-media-thumbnail-size
UPDATE media
SET meta = jsonb_set(COALESCE(meta, '{}'::jsonb), '{thumbnail_size}', to_jsonb($2::bigint), true),
    updated_at = NOW()
WHERE id = $1;
