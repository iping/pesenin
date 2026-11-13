CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL UNIQUE,
    name text NOT NULL DEFAULT '',
    avatar_url text,
    google_sub text UNIQUE,
    is_platform_admin boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE auth_identities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider text NOT NULL,
    provider_subject text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_subject)
);

CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash text NOT NULL UNIQUE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    user_agent text,
    ip text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);

CREATE TABLE accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id uuid NOT NULL UNIQUE REFERENCES users (id),
    name text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    brand_slots int NOT NULL DEFAULT 1,
    store_slots int NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE brands (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid NOT NULL REFERENCES accounts (id),
    name text NOT NULL,
    slug text NOT NULL UNIQUE,
    logo_url text,
    wa_number text,
    description text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('draft', 'active', 'archived')),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT brands_slug_format CHECK (
        slug ~ '^[a-z0-9][a-z0-9-]{1,28}[a-z0-9]$' AND slug !~ '--'
    )
);
CREATE INDEX brands_account_id_idx ON brands (account_id);

CREATE TABLE stores (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    brand_id uuid NOT NULL REFERENCES brands (id) ON DELETE CASCADE,
    name text NOT NULL,
    slug text NOT NULL,
    address text NOT NULL DEFAULT '',
    maps_url text,
    timezone text NOT NULL DEFAULT 'Asia/Jakarta',
    is_open boolean NOT NULL DEFAULT true,
    accept_preorders_when_closed boolean NOT NULL DEFAULT true,
    is_default boolean NOT NULL DEFAULT false,
    archived_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (brand_id, slug),
    CONSTRAINT stores_slug_format CHECK (
        slug ~ '^[a-z0-9][a-z0-9-]{1,28}[a-z0-9]$' AND slug !~ '--'
    )
);

CREATE TABLE account_members (
    account_id uuid NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role text NOT NULL DEFAULT 'owner' CHECK (role IN ('owner', 'brand_admin', 'store_staff')),
    brand_id uuid REFERENCES brands (id),
    store_id uuid REFERENCES stores (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, user_id)
);

CREATE TABLE bank_accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    brand_id uuid NOT NULL REFERENCES brands (id) ON DELETE CASCADE,
    bank_code text NOT NULL,
    account_number text NOT NULL,
    holder_name text NOT NULL,
    is_default boolean NOT NULL DEFAULT false,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX bank_accounts_brand_idx ON bank_accounts (brand_id);

CREATE TABLE products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    brand_id uuid NOT NULL REFERENCES brands (id) ON DELETE CASCADE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    photo_url text,
    base_price bigint NOT NULL CHECK (base_price >= 0),
    active boolean NOT NULL DEFAULT true,
    sort_order int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX products_brand_idx ON products (brand_id);

CREATE TABLE product_variants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id uuid NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    name text NOT NULL,
    price_delta bigint NOT NULL DEFAULT 0,
    active boolean NOT NULL DEFAULT true,
    sort_order int NOT NULL DEFAULT 0
);

CREATE TABLE extra_charges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    brand_id uuid NOT NULL REFERENCES brands (id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 30),
    amount_rp bigint NOT NULL CHECK (amount_rp >= 0),
    fulfilment_methods text[] NOT NULL DEFAULT ARRAY['pickup', 'delivery'],
    sort_order int NOT NULL DEFAULT 0,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX extra_charges_brand_idx ON extra_charges (brand_id);

CREATE TABLE po_batches (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id uuid NOT NULL REFERENCES stores (id),
    brand_id uuid NOT NULL REFERENCES brands (id),
    code text NOT NULL UNIQUE,
    title text NOT NULL,
    status text NOT NULL CHECK (status IN ('draft', 'scheduled', 'open', 'closed', 'completed')),
    opens_at timestamptz NOT NULL,
    closes_at timestamptz NOT NULL,
    ready_at timestamptz NOT NULL,
    payment_deadline_mins int,
    quota_total int,
    allow_pickup boolean NOT NULL DEFAULT true,
    allow_delivery boolean NOT NULL DEFAULT false,
    notes text NOT NULL DEFAULT '',
    closed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX po_batches_store_idx ON po_batches (store_id, created_at DESC);
CREATE INDEX po_batches_status_idx ON po_batches (status, opens_at, closes_at);

CREATE TABLE batch_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id uuid NOT NULL REFERENCES po_batches (id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES products (id),
    variant_id uuid REFERENCES product_variants (id),
    product_name text NOT NULL,
    variant_name text,
    price bigint NOT NULL CHECK (price >= 0),
    quota int,
    sold_count int NOT NULL DEFAULT 0 CHECK (sold_count >= 0),
    sort_order int NOT NULL DEFAULT 0
);
CREATE INDEX batch_items_batch_idx ON batch_items (batch_id);

CREATE TABLE customers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    brand_id uuid NOT NULL REFERENCES brands (id) ON DELETE CASCADE,
    name text NOT NULL,
    wa_number_e164 text NOT NULL,
    first_order_at timestamptz,
    last_order_at timestamptz,
    orders_count int NOT NULL DEFAULT 0,
    total_spent bigint NOT NULL DEFAULT 0,
    UNIQUE (brand_id, wa_number_e164)
);

CREATE SEQUENCE order_code_seq START 1001;
CREATE SEQUENCE topup_code_seq START 1001;

CREATE TABLE orders (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id uuid NOT NULL REFERENCES po_batches (id),
    brand_id uuid NOT NULL REFERENCES brands (id),
    store_id uuid NOT NULL REFERENCES stores (id),
    customer_id uuid NOT NULL REFERENCES customers (id),
    code text NOT NULL UNIQUE,
    public_token text NOT NULL UNIQUE,
    idempotency_key text UNIQUE,
    subtotal bigint NOT NULL,
    extra_charges_total bigint NOT NULL,
    base_total bigint NOT NULL,
    unique_suffix int NOT NULL,
    total_due bigint NOT NULL,
    bank_account_id uuid NOT NULL REFERENCES bank_accounts (id),
    payment_status text NOT NULL CHECK (payment_status IN ('pending', 'proof_submitted', 'paid', 'expired', 'cancelled')),
    fulfilment_status text NOT NULL DEFAULT 'not_ready' CHECK (fulfilment_status IN ('not_ready', 'ready', 'completed', 'no_show')),
    fulfilment_method text NOT NULL CHECK (fulfilment_method IN ('pickup', 'delivery')),
    address text NOT NULL DEFAULT '',
    note text NOT NULL DEFAULT '',
    expires_at timestamptz,
    paid_at timestamptz,
    charged_at timestamptz,
    cancelled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Active orders cannot share a transfer total on the same bank account.
-- Expired and cancelled totals stay reserved in application code for 48 hours.
CREATE UNIQUE INDEX ux_orders_active_amount ON orders (bank_account_id, total_due)
    WHERE payment_status IN ('pending', 'proof_submitted');
CREATE INDEX orders_batch_idx ON orders (batch_id, created_at DESC);
CREATE INDEX orders_brand_idx ON orders (brand_id, created_at DESC);
CREATE INDEX orders_expiry_idx ON orders (expires_at)
    WHERE payment_status IN ('pending', 'proof_submitted');

CREATE TABLE order_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id uuid NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    batch_item_id uuid NOT NULL REFERENCES batch_items (id),
    product_name text NOT NULL,
    variant_name text,
    unit_price bigint NOT NULL,
    qty int NOT NULL CHECK (qty > 0)
);
CREATE INDEX order_items_order_idx ON order_items (order_id);

CREATE TABLE order_charges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id uuid NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    extra_charge_id uuid,
    name text NOT NULL,
    amount_rp bigint NOT NULL,
    computed_rp bigint NOT NULL,
    sort_order int NOT NULL DEFAULT 0
);
CREATE INDEX order_charges_order_idx ON order_charges (order_id);

CREATE TABLE wallets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid NOT NULL UNIQUE REFERENCES accounts (id),
    balance_koin bigint NOT NULL DEFAULT 0,
    version bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE wallet_transactions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id uuid NOT NULL REFERENCES wallets (id),
    type text NOT NULL CHECK (type IN (
        'promo_free_credit', 'topup_credit', 'order_debit', 'slot_purchase',
        'refund_credit', 'referral_reward', 'achievement_reward', 'adjustment'
    )),
    amount_koin bigint NOT NULL,
    balance_after bigint NOT NULL,
    idempotency_key text NOT NULL UNIQUE,
    order_id uuid,
    topup_id uuid,
    brand_id uuid,
    actor_user_id uuid,
    reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX wallet_tx_wallet_idx ON wallet_transactions (wallet_id, created_at DESC);

CREATE FUNCTION forbid_wallet_tx_mutate() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'wallet_transactions is append-only';
END $$;

CREATE TRIGGER wallet_tx_no_update
    BEFORE UPDATE OR DELETE ON wallet_transactions
    FOR EACH ROW EXECUTE FUNCTION forbid_wallet_tx_mutate();

CREATE TABLE platform_bank_accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bank_code text NOT NULL,
    account_number text NOT NULL,
    holder_name text NOT NULL,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE admin_fee_configs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    channel text NOT NULL DEFAULT 'manual_transfer',
    fixed_rp bigint NOT NULL DEFAULT 0,
    percent_bps int NOT NULL DEFAULT 0,
    min_fee_rp bigint NOT NULL DEFAULT 0,
    max_fee_rp bigint,
    effective_from timestamptz NOT NULL DEFAULT now(),
    created_by uuid,
    note text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX admin_fee_configs_channel_idx ON admin_fee_configs (channel, effective_from DESC);

CREATE TABLE topups (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid NOT NULL REFERENCES accounts (id),
    code text NOT NULL UNIQUE,
    channel text NOT NULL DEFAULT 'manual_transfer',
    amount_rp bigint NOT NULL,
    unique_suffix int NOT NULL,
    admin_fee_rp bigint NOT NULL,
    total_due_rp bigint NOT NULL,
    koin_to_credit bigint NOT NULL,
    fee_config_id uuid NOT NULL REFERENCES admin_fee_configs (id),
    fee_fixed_rp bigint NOT NULL,
    fee_percent_bps int NOT NULL DEFAULT 0,
    platform_bank_account_id uuid NOT NULL REFERENCES platform_bank_accounts (id),
    status text NOT NULL CHECK (status IN ('pending', 'awaiting_verification', 'approved', 'rejected', 'expired')),
    proof_key text,
    expires_at timestamptz NOT NULL,
    submitted_at timestamptz,
    received_amount_rp bigint,
    reviewed_by uuid,
    reviewed_at timestamptz,
    reject_reason text,
    idempotency_key text UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ux_topups_active_amount ON topups (platform_bank_account_id, total_due_rp)
    WHERE status IN ('pending', 'awaiting_verification');
CREATE INDEX topups_account_idx ON topups (account_id, created_at DESC);
CREATE INDEX topups_status_idx ON topups (status, created_at);

CREATE TABLE payment_proofs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id uuid REFERENCES orders (id) ON DELETE CASCADE,
    topup_id uuid REFERENCES topups (id) ON DELETE CASCADE,
    object_key text NOT NULL,
    mime text NOT NULL,
    size_bytes int NOT NULL,
    uploaded_at timestamptz NOT NULL DEFAULT now(),
    review_status text NOT NULL DEFAULT 'pending' CHECK (review_status IN ('pending', 'accepted', 'rejected')),
    reject_reason text,
    reviewed_by uuid,
    CHECK (order_id IS NOT NULL OR topup_id IS NOT NULL)
);

CREATE TABLE platform_settings (
    key text PRIMARY KEY,
    value jsonb NOT NULL,
    updated_by uuid,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE push_subscriptions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    endpoint text NOT NULL UNIQUE,
    p256dh text NOT NULL,
    auth_key text NOT NULL,
    user_agent text,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_success_at timestamptz,
    failure_count int NOT NULL DEFAULT 0
);
CREATE INDEX push_subscriptions_user_idx ON push_subscriptions (user_id);

CREATE TABLE notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    account_id uuid,
    brand_id uuid,
    event_type text NOT NULL,
    title text NOT NULL,
    body text NOT NULL,
    deep_link text NOT NULL DEFAULT '',
    dedupe_key text NOT NULL UNIQUE,
    push_eligible boolean NOT NULL DEFAULT false,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed', 'skipped')),
    attempts int NOT NULL DEFAULT 0,
    sent_at timestamptz,
    read_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_recipient_idx ON notifications (recipient_user_id, created_at DESC);
CREATE INDEX notifications_pending_idx ON notifications (status) WHERE status = 'pending' AND push_eligible;

CREATE TABLE message_templates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    brand_id uuid NOT NULL REFERENCES brands (id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('confirmation', 'reminder', 'ready')),
    body text NOT NULL,
    UNIQUE (brand_id, kind)
);

CREATE TABLE audit_logs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid,
    actor_user_id uuid,
    action text NOT NULL,
    entity_type text NOT NULL,
    entity_id text NOT NULL DEFAULT '',
    before jsonb,
    after jsonb,
    ip text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_created_idx ON audit_logs (created_at DESC);

INSERT INTO platform_settings (key, value) VALUES
    ('topup_presets', '[25000, 50000, 100000]'),
    ('custom_min', '25000'),
    ('custom_max', '1000000'),
    ('custom_multiple', '1000'),
    ('topup_expiry_hours', '24'),
    ('order_fee_koin', '100'),
    ('signup_free_koin', '5000'),
    ('grace_floor_koin', '-5000'),
    ('low_balance_koin', '500'),
    ('suffix_reserve_hours', '48'),
    ('payment_deadline_hours', '24');

-- Default is a placeholder. Iping still decides the launch fee (Rp 0 to Rp 2.500).
INSERT INTO admin_fee_configs (channel, fixed_rp, percent_bps, min_fee_rp, note)
VALUES ('manual_transfer', 1000, 0, 0, 'Default P1 fee pending a decision');

INSERT INTO platform_bank_accounts (bank_code, account_number, holder_name)
VALUES ('BCA', '8800123456', 'PT Pesenin Digital');
