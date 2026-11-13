-- name: CreateUser :one
INSERT INTO users (email, name, avatar_url, google_sub, is_platform_admin)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByGoogleSub :one
SELECT * FROM users WHERE google_sub = $1;

-- name: UpsertGoogleUser :one
INSERT INTO users (email, name, avatar_url, google_sub)
VALUES ($1, $2, $3, $4)
ON CONFLICT (google_sub) DO UPDATE
SET email = EXCLUDED.email,
    name = CASE WHEN EXCLUDED.name <> '' THEN EXCLUDED.name ELSE users.name END,
    avatar_url = COALESCE(EXCLUDED.avatar_url, users.avatar_url)
RETURNING *;

-- name: InsertIdentity :exec
INSERT INTO auth_identities (user_id, provider, provider_subject)
VALUES ($1, $2, $3)
ON CONFLICT (provider, provider_subject) DO NOTHING;

-- name: CreateSession :one
INSERT INTO sessions (token_hash, user_id, expires_at, user_agent, ip)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSessionByHash :one
SELECT * FROM sessions WHERE token_hash = $1 AND expires_at > now();

-- name: DeleteSessionByHash :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: CreateAccount :one
INSERT INTO accounts (owner_user_id, name)
VALUES ($1, $2)
RETURNING *;

-- name: GetAccountByOwner :one
SELECT * FROM accounts WHERE owner_user_id = $1;

-- name: GetAccountByID :one
SELECT * FROM accounts WHERE id = $1;

-- name: InsertOwnerMember :exec
INSERT INTO account_members (account_id, user_id, role)
VALUES ($1, $2, 'owner')
ON CONFLICT DO NOTHING;

-- name: CreateWallet :one
INSERT INTO wallets (account_id, balance_koin)
VALUES ($1, 0)
RETURNING *;

-- name: GetWalletByAccount :one
SELECT * FROM wallets WHERE account_id = $1;

-- name: GetWalletForUpdate :one
SELECT * FROM wallets WHERE id = $1 FOR UPDATE;

-- name: GetWalletForAccountUpdate :one
SELECT wallets.*
FROM wallets
JOIN accounts ON accounts.id = wallets.account_id
WHERE accounts.id = $1
FOR UPDATE OF wallets;

-- name: UpdateWalletBalance :one
UPDATE wallets
SET balance_koin = $2, version = version + 1, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: InsertWalletTx :one
INSERT INTO wallet_transactions (
    wallet_id, type, amount_koin, balance_after, idempotency_key,
    order_id, topup_id, brand_id, actor_user_id, reason
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING *;

-- name: GetWalletTxByKey :one
SELECT * FROM wallet_transactions WHERE idempotency_key = $1;

-- name: ListWalletTx :many
SELECT * FROM wallet_transactions
WHERE wallet_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: SumWalletTx :one
SELECT COALESCE(SUM(amount_koin), 0)::bigint AS total
FROM wallet_transactions
WHERE wallet_id = $1;
