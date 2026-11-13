-- name: CreateBatch :one
INSERT INTO po_batches (
    store_id, brand_id, code, title, status, opens_at, closes_at, ready_at,
    payment_deadline_mins, payment_deadline_at, pickup_slots, quota_total, allow_pickup, allow_delivery, notes
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: UpdateBatch :one
UPDATE po_batches
SET title = $2,
    opens_at = $3,
    closes_at = $4,
    ready_at = $5,
    payment_deadline_mins = $6,
    payment_deadline_at = $7,
    pickup_slots = $8,
    quota_total = $9,
    allow_pickup = $10,
    allow_delivery = $11,
    notes = $12,
    status = $13
WHERE id = $1
RETURNING *;

-- name: GetBatch :one
SELECT * FROM po_batches WHERE id = $1;

-- name: GetBatchForUpdate :one
SELECT * FROM po_batches WHERE id = $1 FOR UPDATE;

-- name: GetBatchByCode :one
SELECT * FROM po_batches WHERE code = $1;

-- name: ListBatchesByBrand :many
SELECT * FROM po_batches WHERE brand_id = $1 AND channel = 'po' ORDER BY created_at DESC;

-- name: ListOpenBatchesByBrand :many
SELECT * FROM po_batches WHERE brand_id = $1 AND status = 'open' AND channel = 'po' ORDER BY closes_at;

-- name: SetBatchStatus :one
UPDATE po_batches
SET status = $2,
    closed_at = CASE WHEN $2 = 'closed' THEN COALESCE(closed_at, now()) ELSE closed_at END
WHERE id = $1
RETURNING *;

-- name: ListDueBatchOpens :many
SELECT * FROM po_batches WHERE status = 'scheduled' AND opens_at <= now() AND channel = 'po';

-- name: ListDueBatchCloses :many
SELECT * FROM po_batches WHERE status = 'open' AND closes_at <= now() AND channel = 'po';

-- name: InsertBatchItem :one
INSERT INTO batch_items (
    batch_id, product_id, variant_id, product_name, variant_name, price, quota, sort_order
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListBatchItems :many
SELECT * FROM batch_items WHERE batch_id = $1 ORDER BY sort_order, product_name;

-- name: ListBatchItemsForUpdate :many
SELECT * FROM batch_items WHERE batch_id = $1 ORDER BY id FOR UPDATE;

-- name: GetBatchItemForUpdate :one
SELECT * FROM batch_items WHERE id = $1 FOR UPDATE;

-- name: AddSoldCount :one
UPDATE batch_items
SET sold_count = sold_count + $2
WHERE id = $1 AND (quota IS NULL OR sold_count + $2 <= quota)
RETURNING *;

-- name: ReleaseSoldCount :exec
UPDATE batch_items
SET sold_count = GREATEST(0, sold_count - $2)
WHERE id = $1;

-- name: DeleteBatchItems :exec
DELETE FROM batch_items WHERE batch_id = $1 AND sold_count = 0;

-- name: CountBatchOrders :one
SELECT count(*)::int AS n FROM orders WHERE batch_id = $1;

-- name: UpsertCustomer :one
INSERT INTO customers (brand_id, name, wa_number_e164, first_order_at, last_order_at, orders_count)
VALUES ($1, $2, $3, now(), now(), 1)
ON CONFLICT (brand_id, wa_number_e164) DO UPDATE
SET name = EXCLUDED.name,
    last_order_at = now(),
    orders_count = customers.orders_count + 1
RETURNING *;

-- name: AddCustomerSpend :exec
UPDATE customers
SET total_spent = total_spent + $2, last_order_at = now()
WHERE id = $1;

-- name: ListCustomers :many
SELECT * FROM customers WHERE brand_id = $1 ORDER BY last_order_at DESC NULLS LAST LIMIT 200;

-- name: GetCustomer :one
SELECT * FROM customers WHERE id = $1;

-- name: NextOrderCode :one
SELECT ('PSN-' || nextval('order_code_seq')::text)::text AS code;

-- name: AdvisoryLock :one
SELECT pg_advisory_xact_lock($1::bigint);

-- name: ListReservedTotals :many
SELECT total_due FROM orders
WHERE bank_account_id = $1
AND (
    payment_status IN ('pending', 'proof_submitted')
    OR (payment_status = 'expired' AND expires_at IS NOT NULL AND expires_at > now() - interval '48 hours')
    OR (payment_status = 'cancelled' AND cancelled_at IS NOT NULL AND cancelled_at > now() - interval '48 hours')
);

-- name: InsertOrder :one
INSERT INTO orders (
    batch_id, brand_id, store_id, customer_id, code, public_token, idempotency_key,
    subtotal, extra_charges_total, base_total, unique_suffix, total_due, bank_account_id,
    payment_status, fulfilment_method, address, note, expires_at,
    pickup_slot_start, pickup_slot_end
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, $11, $12, $13,
    'pending', $14, $15, $16, $17,
    $18, $19
)
RETURNING *;

-- name: GetOrderByIdempotency :one
SELECT * FROM orders WHERE idempotency_key = $1;

-- name: GetOrder :one
SELECT * FROM orders WHERE id = $1;

-- name: GetOrderForUpdate :one
SELECT * FROM orders WHERE id = $1 FOR UPDATE;

-- name: GetOrderByToken :one
SELECT * FROM orders WHERE public_token = $1;

-- name: GetOrderByTokenForUpdate :one
SELECT * FROM orders WHERE public_token = $1 FOR UPDATE;

-- name: ListBrandOrders :many
SELECT * FROM orders
WHERE brand_id = $1
  AND (sqlc.narg('payment_status')::text IS NULL OR payment_status = sqlc.narg('payment_status'))
  AND (sqlc.narg('total_due')::bigint IS NULL OR total_due = sqlc.narg('total_due'))
  AND (sqlc.narg('batch_id')::uuid IS NULL OR batch_id = sqlc.narg('batch_id'))
ORDER BY created_at DESC
LIMIT 200;

-- name: ListOrdersByBatch :many
SELECT * FROM orders WHERE batch_id = $1 ORDER BY created_at;

-- name: ListDueOrders :many
SELECT * FROM orders
WHERE payment_status IN ('pending', 'proof_submitted')
  AND expires_at IS NOT NULL
  AND expires_at <= now()
LIMIT 100;

-- name: SetOrderPayment :one
UPDATE orders
SET payment_status = $2,
    paid_at = $3,
    charged_at = $4,
    cancelled_at = $5
WHERE id = $1
RETURNING *;

-- name: SetOrderFulfilment :one
UPDATE orders SET fulfilment_status = $2 WHERE id = $1 RETURNING *;

-- name: InsertOrderItem :one
INSERT INTO order_items (order_id, batch_item_id, product_name, variant_name, unit_price, qty)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListOrderItems :many
SELECT * FROM order_items WHERE order_id = $1 ORDER BY product_name;

-- name: ListOrderItemsByBatch :many
SELECT order_items.*
FROM order_items
JOIN orders ON orders.id = order_items.order_id
WHERE orders.batch_id = $1
  AND (
    ($2::boolean = true AND orders.payment_status NOT IN ('cancelled', 'expired'))
    OR ($2::boolean = false AND orders.payment_status = 'paid')
  );

-- name: InsertOrderCharge :one
INSERT INTO order_charges (order_id, extra_charge_id, name, amount_rp, computed_rp, sort_order)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListOrderCharges :many
SELECT * FROM order_charges WHERE order_id = $1 ORDER BY sort_order;

-- name: InsertProof :one
INSERT INTO payment_proofs (order_id, topup_id, object_key, mime, size_bytes)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListProofsByOrder :many
SELECT * FROM payment_proofs WHERE order_id = $1 ORDER BY uploaded_at DESC;

-- name: SetProofReview :exec
UPDATE payment_proofs
SET review_status = $2, reject_reason = $3, reviewed_by = $4
WHERE order_id = $1 AND review_status = 'pending';

-- name: MarkOrderProof :one
UPDATE orders SET payment_status = 'proof_submitted' WHERE id = $1 RETURNING *;
