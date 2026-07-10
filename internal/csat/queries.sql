-- name: insert
INSERT INTO csat_responses (conversation_id)
VALUES ($1)
ON CONFLICT (conversation_id) DO NOTHING
RETURNING uuid;

-- name: get
SELECT id,
    uuid,
    created_at,
    updated_at,
    conversation_id,
    rating,
    feedback,
    meta,
    response_timestamp
FROM csat_responses
WHERE uuid = $1;

-- name: update
UPDATE csat_responses
SET rating = $2,
    feedback = $3,
    meta = COALESCE($4::jsonb, '{}'),
    response_timestamp = NOW()
WHERE uuid = $1 AND response_timestamp IS NULL;
