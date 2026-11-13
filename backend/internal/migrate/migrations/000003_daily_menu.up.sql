-- Daily menu (catering), phase 1.
--
-- A brand keeps a weekly template. Each store has its own calendar: a date
-- with a row is an override (or a copied week); a date without a row follows
-- the template weekday. Customers order one service date and one meal slot.
--
-- Phase 2 packages (5-day / 20-day, skip and pause) should add
-- menu_subscriptions and point menu_order_days.subscription_id at them.
-- One fulfilled day stays one orders row, so skip/pause is a status on the
-- planned date rather than a second checkout model. subscription_id is
-- reserved and unused in this phase.

CREATE TABLE menu_templates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    brand_id uuid NOT NULL REFERENCES brands (id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX menu_templates_brand_idx ON menu_templates (brand_id);

CREATE TABLE menu_template_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id uuid NOT NULL REFERENCES menu_templates (id) ON DELETE CASCADE,
    weekday smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    meal_slot text NOT NULL DEFAULT '' CHECK (meal_slot IN ('', 'breakfast', 'lunch', 'dinner')),
    product_id uuid NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    variant_id uuid REFERENCES product_variants (id) ON DELETE SET NULL,
    sort_order int NOT NULL DEFAULT 0,
    quota int CHECK (quota IS NULL OR quota >= 0)
);
CREATE INDEX menu_template_items_tpl_idx ON menu_template_items (template_id, weekday, sort_order);

CREATE TABLE store_menu_settings (
    store_id uuid PRIMARY KEY REFERENCES stores (id) ON DELETE CASCADE,
    enabled boolean NOT NULL DEFAULT false,
    template_id uuid REFERENCES menu_templates (id) ON DELETE SET NULL,
    cutoff_days_before int NOT NULL DEFAULT 1 CHECK (cutoff_days_before BETWEEN 0 AND 14),
    cutoff_time time NOT NULL DEFAULT '18:00',
    allow_pickup boolean NOT NULL DEFAULT true,
    allow_delivery boolean NOT NULL DEFAULT true,
    -- Optional portions cap for one calendar day across slots.
    -- Item quotas live on the day items. Null means no day cap here.
    -- TODO(po-capacity): the parallel PO min/max/capacity work should be
    -- consulted via capacity.POCapacity before this number is the only cap.
    day_capacity int CHECK (day_capacity IS NULL OR day_capacity >= 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE store_menu_days (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id uuid NOT NULL REFERENCES stores (id) ON DELETE CASCADE,
    menu_date date NOT NULL,
    source text NOT NULL DEFAULT 'override' CHECK (source IN ('override', 'copy', 'rotation')),
    closed boolean NOT NULL DEFAULT false,
    notes text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (store_id, menu_date)
);
CREATE INDEX store_menu_days_store_idx ON store_menu_days (store_id, menu_date);

CREATE TABLE store_menu_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    menu_day_id uuid NOT NULL REFERENCES store_menu_days (id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES products (id),
    variant_id uuid REFERENCES product_variants (id) ON DELETE SET NULL,
    meal_slot text NOT NULL DEFAULT '' CHECK (meal_slot IN ('', 'breakfast', 'lunch', 'dinner')),
    sort_order int NOT NULL DEFAULT 0,
    quota int CHECK (quota IS NULL OR quota >= 0),
    sold_count int NOT NULL DEFAULT 0 CHECK (sold_count >= 0)
);
CREATE INDEX store_menu_items_day_idx ON store_menu_items (menu_day_id);

CREATE TABLE product_nutrition (
    product_id uuid PRIMARY KEY REFERENCES products (id) ON DELETE CASCADE,
    kcal int CHECK (kcal IS NULL OR kcal >= 0),
    protein_g double precision CHECK (protein_g IS NULL OR protein_g >= 0),
    carb_g double precision CHECK (carb_g IS NULL OR carb_g >= 0),
    fat_g double precision CHECK (fat_g IS NULL OR fat_g >= 0),
    allergens text[] NOT NULL DEFAULT '{}',
    tags text[] NOT NULL DEFAULT '{}'
);

CREATE TABLE menu_order_days (
    order_id uuid PRIMARY KEY REFERENCES orders (id) ON DELETE CASCADE,
    store_id uuid NOT NULL REFERENCES stores (id),
    service_date date NOT NULL,
    meal_slot text NOT NULL DEFAULT '' CHECK (meal_slot IN ('', 'breakfast', 'lunch', 'dinner')),
    subscription_id uuid,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX menu_order_days_lookup_idx ON menu_order_days (store_id, service_date, meal_slot);

CREATE TABLE menu_order_lines (
    order_item_id uuid PRIMARY KEY REFERENCES order_items (id) ON DELETE CASCADE,
    menu_item_id uuid NOT NULL REFERENCES store_menu_items (id),
    qty int NOT NULL CHECK (qty > 0)
);
CREATE INDEX menu_order_lines_item_idx ON menu_order_lines (menu_item_id);

-- Hidden PO batch per store so menu orders can reuse orders.batch_id.
-- Listed queries filter channel = 'po'. Closes far in the future so the
-- batch worker does not treat the anchor as a pre-order window.
ALTER TABLE po_batches ADD COLUMN channel text NOT NULL DEFAULT 'po' CHECK (channel IN ('po', 'menu'));
CREATE UNIQUE INDEX ux_po_batches_menu_anchor ON po_batches (store_id) WHERE channel = 'menu';
