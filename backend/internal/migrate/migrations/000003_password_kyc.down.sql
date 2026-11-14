DROP TABLE IF EXISTS brand_kyc_documents;
DROP TABLE IF EXISTS brand_kyc_events;
DROP TABLE IF EXISTS whatsapp_otps;
DROP TABLE IF EXISTS auth_tokens;

ALTER TABLE brands DROP COLUMN IF EXISTS kyc_updated_at;
ALTER TABLE brands DROP COLUMN IF EXISTS kyc_reason;
ALTER TABLE brands DROP COLUMN IF EXISTS kyc_status;

ALTER TABLE sessions DROP COLUMN IF EXISTS auth_via;

DROP INDEX IF EXISTS users_whatsapp_e164_key;
ALTER TABLE users DROP COLUMN IF EXISTS whatsapp_verified_at;
ALTER TABLE users DROP COLUMN IF EXISTS whatsapp_e164;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
ALTER TABLE users DROP COLUMN IF EXISTS password_hash;
