-- name: CreateBrand :one
INSERT INTO brands (account_id, name, slug, description, wa_number, status)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetBrandByID :one
SELECT * FROM brands WHERE id = $1;

-- name: GetBrandBySlug :one
SELECT * FROM brands WHERE slug = $1 AND status <> 'archived';

-- name: ListBrandsByAccount :many
SELECT * FROM brands WHERE account_id = $1 AND status <> 'archived' ORDER BY created_at;

-- name: CountBrands :one
SELECT count(*)::int AS n FROM brands WHERE account_id = $1 AND status <> 'archived';

-- name: UpdateBrand :one
UPDATE brands
SET name = $2, description = $3, wa_number = $4, logo_url = $5
WHERE id = $1
RETURNING *;

-- name: SlugTaken :one
SELECT EXISTS(SELECT 1 FROM brands WHERE slug = $1) AS taken;

-- name: CreateStore :one
INSERT INTO stores (brand_id, name, slug, address, maps_url, is_open, accept_preorders_when_closed, is_default)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetStore :one
SELECT * FROM stores WHERE id = $1;

-- name: GetStoreBySlug :one
SELECT * FROM stores WHERE brand_id = $1 AND slug = $2 AND archived_at IS NULL;

-- name: ListStores :many
SELECT * FROM stores WHERE brand_id = $1 AND archived_at IS NULL ORDER BY is_default DESC, created_at;

-- name: UpdateStore :one
UPDATE stores
SET name = $2,
    address = $3,
    maps_url = $4,
    is_open = $5,
    accept_preorders_when_closed = $6
WHERE id = $1
RETURNING *;

-- name: CreateBank :one
INSERT INTO bank_accounts (brand_id, bank_code, account_number, holder_name, is_default, active)
VALUES ($1, $2, $3, $4, $5, true)
RETURNING *;

-- name: ListBanks :many
SELECT * FROM bank_accounts WHERE brand_id = $1 AND active ORDER BY is_default DESC, created_at;

-- name: GetBank :one
SELECT * FROM bank_accounts WHERE id = $1;

-- name: ClearDefaultBank :exec
UPDATE bank_accounts SET is_default = false WHERE brand_id = $1;

-- name: CountBanks :one
SELECT count(*)::int AS n FROM bank_accounts WHERE brand_id = $1 AND active;

-- name: CreateProduct :one
INSERT INTO products (brand_id, name, description, photo_url, base_price, active, sort_order)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateProduct :one
UPDATE products
SET name = $2, description = $3, base_price = $4, active = $5, photo_url = $6, sort_order = $7
WHERE id = $1
RETURNING *;

-- name: GetProduct :one
SELECT * FROM products WHERE id = $1;

-- name: ListProducts :many
SELECT * FROM products WHERE brand_id = $1 ORDER BY sort_order, created_at;

-- name: CreateVariant :one
INSERT INTO product_variants (product_id, name, price_delta, active, sort_order)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListVariantsByProduct :many
SELECT * FROM product_variants WHERE product_id = $1 AND active ORDER BY sort_order;

-- name: ListVariantsByBrand :many
SELECT product_variants.*
FROM product_variants
JOIN products ON products.id = product_variants.product_id
WHERE products.brand_id = $1 AND product_variants.active
ORDER BY product_variants.sort_order;

-- name: DeleteVariants :exec
DELETE FROM product_variants WHERE product_id = $1;

-- name: CreateCharge :one
INSERT INTO extra_charges (brand_id, name, amount_rp, fulfilment_methods, sort_order, active)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateCharge :one
UPDATE extra_charges
SET name = $2, amount_rp = $3, fulfilment_methods = $4, active = $5, sort_order = $6
WHERE id = $1
RETURNING *;

-- name: ListCharges :many
SELECT * FROM extra_charges WHERE brand_id = $1 ORDER BY sort_order, created_at;

-- name: ListActiveCharges :many
SELECT * FROM extra_charges WHERE brand_id = $1 AND active ORDER BY sort_order, created_at;

-- name: CountActiveCharges :one
SELECT count(*)::int AS n FROM extra_charges WHERE brand_id = $1 AND active;

-- name: GetCharge :one
SELECT * FROM extra_charges WHERE id = $1;

-- name: UpsertTemplate :one
INSERT INTO message_templates (brand_id, kind, body)
VALUES ($1, $2, $3)
ON CONFLICT (brand_id, kind) DO UPDATE SET body = EXCLUDED.body
RETURNING *;

-- name: ListTemplates :many
SELECT * FROM message_templates WHERE brand_id = $1 ORDER BY kind;

-- name: GetTemplate :one
SELECT * FROM message_templates WHERE brand_id = $1 AND kind = $2;
