-- PO capacity rules. Brand defaults, per-store overrides, and the snapshot used by a batch.
-- Qty still lives on po_batches.quota_total and batch_items.sold_count so the existing
-- conditional UPDATE keeps working. These tables add the rules that snapshot does not have.
-- Store opening hours are intentionally not stored here.

CREATE TABLE po_capacity_rules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    brand_id uuid NOT NULL REFERENCES brands (id) ON DELETE CASCADE,
    store_id uuid REFERENCES stores (id) ON DELETE CASCADE,
    min_qty int CHECK (min_qty IS NULL OR min_qty > 0),
    min_amount_rp bigint CHECK (min_amount_rp IS NULL OR min_amount_rp > 0),
    max_qty_per_customer int CHECK (max_qty_per_customer IS NULL OR max_qty_per_customer > 0),
    capacity_qty int CHECK (capacity_qty IS NULL OR capacity_qty >= 0),
    capacity_orders int CHECK (capacity_orders IS NULL OR capacity_orders >= 0),
    min_proceed_qty int CHECK (min_proceed_qty IS NULL OR min_proceed_qty > 0),
    cutoff_lead_minutes int CHECK (cutoff_lead_minutes IS NULL OR cutoff_lead_minutes >= 0),
    waitlist_enabled boolean,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX po_capacity_rules_brand_uq
    ON po_capacity_rules (brand_id) WHERE store_id IS NULL;
CREATE UNIQUE INDEX po_capacity_rules_store_uq
    ON po_capacity_rules (store_id) WHERE store_id IS NOT NULL;

CREATE TABLE batch_capacities (
    batch_id uuid PRIMARY KEY REFERENCES po_batches (id) ON DELETE CASCADE,
    min_qty int CHECK (min_qty IS NULL OR min_qty > 0),
    min_amount_rp bigint CHECK (min_amount_rp IS NULL OR min_amount_rp > 0),
    max_qty_per_customer int CHECK (max_qty_per_customer IS NULL OR max_qty_per_customer > 0),
    capacity_orders int CHECK (capacity_orders IS NULL OR capacity_orders >= 0),
    reserved_orders int NOT NULL DEFAULT 0 CHECK (reserved_orders >= 0),
    confirmed_orders int NOT NULL DEFAULT 0 CHECK (confirmed_orders >= 0),
    min_proceed_qty int CHECK (min_proceed_qty IS NULL OR min_proceed_qty > 0),
    cutoff_lead_minutes int CHECK (cutoff_lead_minutes IS NULL OR cutoff_lead_minutes >= 0),
    waitlist_enabled boolean NOT NULL DEFAULT false,
    closed_reason text NOT NULL DEFAULT '' CHECK (closed_reason IN ('', 'full', 'seller', 'schedule')),
    quota_state text NOT NULL DEFAULT 'ok' CHECK (quota_state IN ('ok', 'below_min', 'proceed', 'cancelled')),
    notified_at timestamptz,
    decided_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE batch_item_limits (
    batch_item_id uuid PRIMARY KEY REFERENCES batch_items (id) ON DELETE CASCADE,
    min_qty int CHECK (min_qty IS NULL OR min_qty > 0),
    max_qty_per_customer int CHECK (max_qty_per_customer IS NULL OR max_qty_per_customer > 0)
);

CREATE TABLE product_capacity_rules (
    product_id uuid PRIMARY KEY REFERENCES products (id) ON DELETE CASCADE,
    min_qty int CHECK (min_qty IS NULL OR min_qty > 0),
    max_qty_per_customer int CHECK (max_qty_per_customer IS NULL OR max_qty_per_customer > 0),
    capacity_qty int CHECK (capacity_qty IS NULL OR capacity_qty >= 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Per pickup slot. A missing row means that slot has no extra cap.
CREATE TABLE pickup_capacities (
    batch_id uuid NOT NULL REFERENCES po_batches (id) ON DELETE CASCADE,
    slot_start timestamptz NOT NULL,
    capacity_qty int CHECK (capacity_qty IS NULL OR capacity_qty >= 0),
    capacity_orders int CHECK (capacity_orders IS NULL OR capacity_orders >= 0),
    reserved_qty int NOT NULL DEFAULT 0 CHECK (reserved_qty >= 0),
    reserved_orders int NOT NULL DEFAULT 0 CHECK (reserved_orders >= 0),
    confirmed_qty int NOT NULL DEFAULT 0 CHECK (confirmed_qty >= 0),
    confirmed_orders int NOT NULL DEFAULT 0 CHECK (confirmed_orders >= 0),
    PRIMARY KEY (batch_id, slot_start)
);

CREATE TABLE po_waitlist (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id uuid NOT NULL REFERENCES po_batches (id) ON DELETE CASCADE,
    batch_item_id uuid REFERENCES batch_items (id) ON DELETE SET NULL,
    slot_start timestamptz,
    name text NOT NULL,
    wa_number_e164 text NOT NULL,
    qty int NOT NULL CHECK (qty > 0),
    public_token text NOT NULL UNIQUE,
    status text NOT NULL DEFAULT 'waiting' CHECK (status IN ('waiting', 'notified', 'converted', 'cancelled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    notified_at timestamptz
);

CREATE INDEX po_waitlist_batch_idx ON po_waitlist (batch_id, status, created_at);
CREATE UNIQUE INDEX po_waitlist_active_wa
    ON po_waitlist (batch_id, wa_number_e164) WHERE status = 'waiting';

CREATE TABLE customer_notices (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id uuid REFERENCES orders (id) ON DELETE CASCADE,
    batch_id uuid NOT NULL REFERENCES po_batches (id) ON DELETE CASCADE,
    wa_number_e164 text NOT NULL DEFAULT '',
    kind text NOT NULL,
    title text NOT NULL,
    body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX customer_notices_order_idx ON customer_notices (order_id);
CREATE INDEX customer_notices_batch_idx ON customer_notices (batch_id, created_at DESC);

-- Customer transfer refunds are marked, not sent. Koin refunds stay in the ledger.
CREATE TABLE order_refund_flags (
    order_id uuid PRIMARY KEY REFERENCES orders (id) ON DELETE CASCADE,
    batch_id uuid NOT NULL REFERENCES po_batches (id),
    store_id uuid NOT NULL REFERENCES stores (id),
    reason text NOT NULL,
    marked_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX order_refund_flags_store_idx ON order_refund_flags (store_id);

-- oversell_blocked: a checkout that would have exceeded capacity.
-- min_quota_cancel: an order cancelled because the batch missed its minimum.
CREATE TABLE capacity_incidents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id uuid NOT NULL REFERENCES stores (id) ON DELETE CASCADE,
    brand_id uuid NOT NULL REFERENCES brands (id) ON DELETE CASCADE,
    batch_id uuid,
    kind text NOT NULL CHECK (kind IN ('oversell_blocked', 'min_quota_cancel')),
    qty int NOT NULL DEFAULT 1 CHECK (qty > 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX capacity_incidents_store_idx ON capacity_incidents (store_id, kind);

-- Existing batches keep today's behaviour: no mins, unlimited order-count, counters filled from live orders.
INSERT INTO batch_capacities (batch_id, reserved_orders, confirmed_orders)
SELECT b.id,
    (SELECT count(*)::int FROM orders o
        WHERE o.batch_id = b.id AND o.payment_status NOT IN ('cancelled', 'expired')),
    (SELECT count(*)::int FROM orders o
        WHERE o.batch_id = b.id AND o.payment_status IN ('proof_submitted', 'paid'))
FROM po_batches b
ON CONFLICT (batch_id) DO NOTHING;
