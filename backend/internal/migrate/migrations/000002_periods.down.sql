ALTER TABLE orders DROP COLUMN IF EXISTS pickup_slot_end;
ALTER TABLE orders DROP COLUMN IF EXISTS pickup_slot_start;
ALTER TABLE po_batches DROP COLUMN IF EXISTS payment_deadline_at;
ALTER TABLE po_batches DROP COLUMN IF EXISTS pickup_slots;
