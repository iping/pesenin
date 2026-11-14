-- Email/password credentials, contact verification, and brand KYC.
-- Password hashes and one-time tokens stay out of the users row scans that sqlc owns;
-- these columns are read with explicit queries.

ALTER TABLE users
    ADD COLUMN password_hash text,
    ADD COLUMN email_verified_at timestamptz,
    ADD COLUMN whatsapp_e164 text,
    ADD COLUMN whatsapp_verified_at timestamptz;

CREATE UNIQUE INDEX users_whatsapp_e164_key ON users (whatsapp_e164) WHERE whatsapp_e164 IS NOT NULL;

-- Google sign-in already required a verified Google email.
UPDATE users
SET email_verified_at = now()
WHERE google_sub IS NOT NULL AND email_verified_at IS NULL;

ALTER TABLE sessions
    ADD COLUMN auth_via text NOT NULL DEFAULT '';

ALTER TABLE brands
    ADD COLUMN kyc_status text NOT NULL DEFAULT 'unverified'
        CHECK (kyc_status IN ('unverified', 'contact_verified', 'pending_review', 'verified', 'rejected', 'suspended')),
    ADD COLUMN kyc_reason text NOT NULL DEFAULT '',
    ADD COLUMN kyc_updated_at timestamptz NOT NULL DEFAULT now();

CREATE INDEX brands_kyc_status_idx ON brands (kyc_status, kyc_updated_at DESC);

CREATE TABLE auth_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose text NOT NULL CHECK (purpose IN ('verify_email', 'reset_password', 'link_password')),
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auth_tokens_user_purpose_idx ON auth_tokens (user_id, purpose);

CREATE TABLE whatsapp_otps (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    phone_e164 text NOT NULL,
    code_hash text NOT NULL,
    salt text NOT NULL,
    expires_at timestamptz NOT NULL,
    attempts int NOT NULL DEFAULT 0,
    max_attempts int NOT NULL DEFAULT 5,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX whatsapp_otps_user_idx ON whatsapp_otps (user_id, created_at DESC);

CREATE TABLE brand_kyc_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    brand_id uuid NOT NULL REFERENCES brands (id) ON DELETE CASCADE,
    from_status text NOT NULL,
    to_status text NOT NULL,
    reason text NOT NULL DEFAULT '',
    actor_user_id uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX brand_kyc_events_brand_idx ON brand_kyc_events (brand_id, created_at DESC);

-- Reserved for a later KTP/NIB review. Nothing writes this table yet.
CREATE TABLE brand_kyc_documents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    brand_id uuid NOT NULL REFERENCES brands (id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('ktp', 'nib')),
    storage_key text,
    status text NOT NULL DEFAULT 'absent' CHECK (status IN ('absent', 'submitted', 'accepted', 'rejected')),
    note text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
