-- TIME-1..4: exact instants, pickup slots, and the slot snapshotted on each order.
ALTER TABLE po_batches ADD COLUMN pickup_slots jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE po_batches ADD COLUMN payment_deadline_at timestamptz;

UPDATE po_batches
SET pickup_slots = jsonb_build_array(
    jsonb_build_object(
        'start', to_jsonb(ready_at),
        'end', to_jsonb(ready_at + interval '3 hours')
    )
)
WHERE pickup_slots = '[]'::jsonb;

ALTER TABLE orders ADD COLUMN pickup_slot_start timestamptz;
ALTER TABLE orders ADD COLUMN pickup_slot_end timestamptz;
