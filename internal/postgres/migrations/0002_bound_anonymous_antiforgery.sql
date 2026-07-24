CREATE TABLE anonymous_antiforgery_keys (
    id smallint NOT NULL,
    key_material bytea NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT pk_anonymous_antiforgery_keys PRIMARY KEY (id),
    CONSTRAINT ck_anonymous_antiforgery_key_id CHECK (id = 1),
    CONSTRAINT ck_anonymous_antiforgery_key_length CHECK (octet_length(key_material) = 32)
);

DELETE FROM antiforgery_tokens WHERE session_token_hash IS NULL;

ALTER TABLE antiforgery_tokens
    ALTER COLUMN session_token_hash SET NOT NULL;

DELETE FROM antiforgery_tokens AS duplicate
USING antiforgery_tokens AS retained
WHERE duplicate.session_token_hash = retained.session_token_hash
  AND duplicate.selector_hash < retained.selector_hash;

DROP INDEX ix_antiforgery_tokens_session_hash;

CREATE UNIQUE INDEX ux_antiforgery_tokens_session_hash
    ON antiforgery_tokens (session_token_hash);
