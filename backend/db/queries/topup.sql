-- name: GetSetting :one
SELECT * FROM platform_settings WHERE key = $1;

-- name: ListSettings :many
SELECT * FROM platform_settings ORDER BY key;

-- name: UpsertSetting :one
INSERT INTO platform_settings (key, value, updated_by, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()
RETURNING *;

-- name: GetActiveFeeConfig :one
SELECT * FROM admin_fee_configs
WHERE channel = $1 AND effective_from <= now()
ORDER BY effective_from DESC, created_at DESC
LIMIT 1;

-- name: ListFeeConfigs :many
SELECT * FROM admin_fee_configs
WHERE channel = $1
ORDER BY effective_from DESC, created_at DESC;

-- name: InsertFeeConfig :one
INSERT INTO admin_fee_configs (channel, fixed_rp, percent_bps, min_fee_rp, max_fee_rp, effective_from, created_by, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetActivePlatformBank :one
SELECT * FROM platform_bank_accounts WHERE active ORDER BY created_at LIMIT 1;

-- name: ListPlatformBanks :many
SELECT * FROM platform_bank_accounts ORDER BY created_at;

-- name: NextTopupCode :one
SELECT ('TP-' || nextval('topup_code_seq')::text)::text AS code;

-- name: ListReservedTopupTotals :many
SELECT total_due_rp FROM topups
WHERE platform_bank_account_id = $1
AND (
    status IN ('pending', 'awaiting_verification')
    OR (status = 'expired' AND expires_at > now() - interval '48 hours')
    OR (status = 'rejected' AND reviewed_at IS NOT NULL AND reviewed_at > now() - interval '48 hours')
);

-- name: InsertTopup :one
INSERT INTO topups (
    account_id, code, channel, amount_rp, unique_suffix, admin_fee_rp, total_due_rp,
    koin_to_credit, fee_config_id, fee_fixed_rp, fee_percent_bps, platform_bank_account_id,
    status, expires_at, idempotency_key
) VALUES (
    $1, $2, 'manual_transfer', $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    'pending', $12, $13
)
RETURNING *;

-- name: GetTopup :one
SELECT * FROM topups WHERE id = $1;

-- name: GetTopupForUpdate :one
SELECT * FROM topups WHERE id = $1 FOR UPDATE;

-- name: GetTopupByIdempotency :one
SELECT * FROM topups WHERE idempotency_key = $1;

-- name: ListTopupsByAccount :many
SELECT * FROM topups WHERE account_id = $1 ORDER BY created_at DESC LIMIT 50;

-- name: ListTopupsByStatus :many
SELECT * FROM topups
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at ASC
LIMIT 100;

-- name: ListDueTopups :many
SELECT * FROM topups WHERE status = 'pending' AND expires_at <= now() LIMIT 100;

-- name: SetTopupProof :one
UPDATE topups
SET status = 'awaiting_verification', proof_key = $2, submitted_at = now()
WHERE id = $1
RETURNING *;

-- name: SetTopupStatus :one
UPDATE topups
SET status = $2,
    received_amount_rp = $3,
    reviewed_by = $4,
    reviewed_at = $5,
    reject_reason = $6,
    koin_to_credit = $7
WHERE id = $1
RETURNING *;

-- name: ExpireTopup :one
UPDATE topups SET status = 'expired' WHERE id = $1 AND status = 'pending' RETURNING *;

-- name: InsertNotification :one
INSERT INTO notifications (
    recipient_user_id, account_id, brand_id, event_type, title, body, deep_link, dedupe_key, push_eligible, status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (dedupe_key) DO NOTHING
RETURNING *;

-- name: ListNotifications :many
SELECT * FROM notifications
WHERE recipient_user_id = $1
ORDER BY created_at DESC
LIMIT 50;

-- name: UnreadCount :one
SELECT count(*)::int AS n
FROM notifications
WHERE recipient_user_id = $1 AND read_at IS NULL;

-- name: MarkNotificationsRead :exec
UPDATE notifications
SET read_at = now()
WHERE recipient_user_id = $1 AND read_at IS NULL
  AND (sqlc.narg('id')::uuid IS NULL OR id = sqlc.narg('id'));

-- name: ListPendingPushes :many
SELECT * FROM notifications
WHERE status = 'pending' AND push_eligible
ORDER BY created_at
LIMIT 50;

-- name: SetNotificationStatus :exec
UPDATE notifications
SET status = $2, attempts = attempts + 1, sent_at = CASE WHEN $2 = 'sent' THEN now() ELSE sent_at END
WHERE id = $1;

-- name: UpsertPushSub :one
INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth_key, user_agent)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (endpoint) DO UPDATE
SET user_id = EXCLUDED.user_id,
    p256dh = EXCLUDED.p256dh,
    auth_key = EXCLUDED.auth_key,
    user_agent = EXCLUDED.user_agent,
    failure_count = 0
RETURNING *;

-- name: ListPushSubs :many
SELECT * FROM push_subscriptions WHERE user_id = $1;

-- name: DeletePushSub :exec
DELETE FROM push_subscriptions WHERE id = $1 AND user_id = $2;

-- name: DeletePushByEndpoint :exec
DELETE FROM push_subscriptions WHERE endpoint = $1;

-- name: BumpPushFailure :exec
UPDATE push_subscriptions
SET failure_count = failure_count + 1
WHERE endpoint = $1;

-- name: MarkPushSuccess :exec
UPDATE push_subscriptions
SET failure_count = 0, last_success_at = now()
WHERE endpoint = $1;

-- name: InsertAudit :exec
INSERT INTO audit_logs (account_id, actor_user_id, action, entity_type, entity_id, before, after, ip)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListAudit :many
SELECT * FROM audit_logs
ORDER BY created_at DESC
LIMIT $1;

-- name: AccountOwner :one
SELECT users.*
FROM users
JOIN accounts ON accounts.owner_user_id = users.id
JOIN brands ON brands.account_id = accounts.id
WHERE brands.id = $1;

-- name: SumApprovedTopups :one
SELECT count(*)::int AS topup_count,
       COALESCE(SUM(amount_rp), 0)::bigint AS topup_rp,
       COALESCE(SUM(admin_fee_rp), 0)::bigint AS fee_rp
FROM topups
WHERE status = 'approved' AND reviewed_at >= $1 AND reviewed_at < $2;

-- name: SumLedgerByType :one
SELECT COALESCE(SUM(amount_koin), 0)::bigint AS total
FROM wallet_transactions
WHERE type = $1 AND created_at >= $2 AND created_at < $3;

-- name: SumOutstandingKoin :one
SELECT COALESCE(SUM(balance_koin), 0)::bigint AS total FROM wallets;

-- name: CountWalletsAtOrBelow :one
SELECT count(*)::int AS n FROM wallets WHERE balance_koin <= $1;
