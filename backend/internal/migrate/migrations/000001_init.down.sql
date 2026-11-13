DROP TABLE IF EXISTS audit_logs, message_templates, notifications, push_subscriptions,
    platform_settings, payment_proofs, topups, admin_fee_configs, platform_bank_accounts,
    wallet_transactions, wallets, order_charges, order_items, orders, customers,
    batch_items, po_batches, extra_charges, product_variants, products, bank_accounts,
    account_members, stores, brands, accounts, sessions, auth_identities, users CASCADE;
DROP SEQUENCE IF EXISTS order_code_seq, topup_code_seq;
DROP FUNCTION IF EXISTS forbid_wallet_tx_mutate();
