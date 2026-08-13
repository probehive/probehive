ALTER TABLE status_pages
    ADD COLUMN publication_token_hash bytea,
    ADD COLUMN published_at timestamp with time zone,
    ADD CONSTRAINT ck_status_pages_publication CHECK (
        (publication_token_hash IS NULL AND published_at IS NULL)
        OR
        (publication_token_hash IS NOT NULL AND octet_length(publication_token_hash) = 32 AND published_at IS NOT NULL)
    );

CREATE UNIQUE INDEX ux_status_pages_publication_token_hash
    ON status_pages (publication_token_hash)
    WHERE publication_token_hash IS NOT NULL;
