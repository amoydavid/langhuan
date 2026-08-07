-- 000019_external_identities: OIDC 外部身份绑定。
-- 记录 (issuer, subject) → user 的映射，支持 JIT 建号、email 合并与主动绑定。
-- 一个 user 可绑多个 identity，但 (issuer, subject) 全局唯一。
CREATE TABLE external_identities (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    issuer         text NOT NULL,
    subject        text NOT NULL,
    email          text NOT NULL,
    email_verified boolean NOT NULL DEFAULT false,
    raw_profile    jsonb NOT NULL DEFAULT '{}'::jsonb,
    last_auth_at   timestamptz NOT NULL DEFAULT now(),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (issuer, subject),
    CONSTRAINT external_identities_issuer_nonempty CHECK (btrim(issuer) <> ''),
    CONSTRAINT external_identities_subject_nonempty CHECK (btrim(subject) <> ''),
    CONSTRAINT external_identities_email_nonempty CHECK (btrim(email) <> '')
);

CREATE INDEX idx_external_identities_user_id ON external_identities(user_id);
