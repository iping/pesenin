-- Weekly opening hours, special dates, and manual open/close overrides.
-- Effective status is computed on read (override > special date > weekly).
-- A store with no saved schedule still follows stores.is_open.

CREATE TABLE brand_weekly_hours (
    brand_id uuid NOT NULL REFERENCES brands (id) ON DELETE CASCADE,
    weekday smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    closed boolean NOT NULL DEFAULT false,
    slots jsonb NOT NULL DEFAULT '[]'::jsonb,
    PRIMARY KEY (brand_id, weekday)
);

CREATE TABLE store_hour_prefs (
    store_id uuid PRIMARY KEY REFERENCES stores (id) ON DELETE CASCADE,
    inherit boolean NOT NULL DEFAULT true,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE store_weekly_hours (
    store_id uuid NOT NULL REFERENCES stores (id) ON DELETE CASCADE,
    weekday smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    closed boolean NOT NULL DEFAULT false,
    slots jsonb NOT NULL DEFAULT '[]'::jsonb,
    PRIMARY KEY (store_id, weekday)
);

CREATE TABLE special_hours (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    brand_id uuid NOT NULL REFERENCES brands (id) ON DELETE CASCADE,
    store_id uuid REFERENCES stores (id) ON DELETE CASCADE,
    date_from date NOT NULL,
    date_to date NOT NULL,
    closed boolean NOT NULL DEFAULT false,
    slots jsonb NOT NULL DEFAULT '[]'::jsonb,
    note text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT special_hours_range CHECK (date_to >= date_from)
);
CREATE INDEX special_hours_brand_idx ON special_hours (brand_id, date_from, date_to);
CREATE INDEX special_hours_store_idx ON special_hours (store_id, date_from, date_to);

CREATE TABLE store_overrides (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id uuid NOT NULL REFERENCES stores (id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('open', 'closed', 'pause')),
    reason text NOT NULL DEFAULT '',
    starts_at timestamptz NOT NULL DEFAULT now(),
    ends_at timestamptz,
    created_by uuid REFERENCES users (id),
    cleared_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX store_overrides_active_idx ON store_overrides (store_id, created_at DESC) WHERE cleared_at IS NULL;
