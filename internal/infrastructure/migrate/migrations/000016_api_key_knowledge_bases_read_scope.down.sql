ALTER TABLE workspace_api_tokens
    DROP CONSTRAINT IF EXISTS workspace_api_tokens_scopes_check;

ALTER TABLE workspace_api_tokens
    ADD CONSTRAINT workspace_api_tokens_scopes_check
    CHECK (
        cardinality(scopes) > 0 AND
        scopes <@ ARRAY[
            'knowledge_bases:write',
            'documents:read',
            'documents:write',
            'search:read'
        ]::text[]
    );

