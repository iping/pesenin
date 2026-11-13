package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestCapacityNoOversellUnderConcurrency(t *testing.T) {
	app, user, batch := fixture(t)
	ctx := context.Background()
	q := int32(5)
	tight, err := app.CreateBatch(ctx, user.ID, batch.BrandID, BatchIn{
		Title: "Ketat", OpensAt: time.Now().Add(-time.Hour), ClosesAt: time.Now().Add(48 * time.Hour),
		ReadyAt: time.Now().Add(72 * time.Hour), AllowPickup: true, Publish: true,
		QuotaTotal: &q, Items: []BatchItemIn{{ProductID: firstProduct(t, app, batch.BrandID), Price: 15000, Quota: &q}},
	})
	if err != nil {
		t.Fatal(err)
	}
	item := firstItem(t, app, tight.ID)
	const n = 16
	var wg sync.WaitGroup
	okN := 0
	var mu sync.Mutex
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := app.PlaceOrder(ctx, tight.Code, PlaceOrder{
				Name: fmt.Sprintf("C%d", i), WhatsApp: fmt.Sprintf("08170000%04d", i), Method: "pickup",
				Items: []CartItem{{BatchItemID: item, Qty: 1}},
			})
			if err == nil {
				mu.Lock()
				okN++
				mu.Unlock()
				return
			}
			if code := errCode(err); code != "quota" && code != "capacity" && code != "batch_closed" {
				errCh <- fmt.Errorf("goroutine %d: %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if okN != int(q) {
		t.Fatalf("successes %d want %d", okN, q)
	}
	var sold int32
	if err := app.Pool.QueryRow(ctx, `SELECT sold_count FROM batch_items WHERE batch_id = $1`, tight.ID).Scan(&sold); err != nil {
		t.Fatal(err)
	}
	if sold != q {
		t.Fatalf("sold %d", sold)
	}
	var reserved int32
	if err := app.Pool.QueryRow(ctx, `SELECT reserved_orders FROM batch_capacities WHERE batch_id = $1`, tight.ID).Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if reserved != q {
		t.Fatalf("reserved orders %d", reserved)
	}
	stats, err := app.CapacityStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var blocked, over int32
	for _, s := range stats {
		if s.StoreID == tight.StoreID {
			blocked = s.OversellBlocked
			over = s.OversoldNow
		}
	}
	if blocked < 1 {
		t.Fatalf("oversell blocked %d", blocked)
	}
	if over != 0 {
		t.Fatalf("oversold now %d", over)
	}
}

func TestCapacityOrderCount(t *testing.T) {
	app, user, batch := fixture(t)
	ctx := context.Background()
	orders := int32(3)
	wide := int32(50)
	b, err := app.CreateBatch(ctx, user.ID, batch.BrandID, BatchIn{
		Title: "Hitung", OpensAt: time.Now().Add(-time.Hour), ClosesAt: time.Now().Add(48 * time.Hour),
		ReadyAt: time.Now().Add(72 * time.Hour), AllowPickup: true, Publish: true, RulesSet: true,
		QuotaTotal: &wide, CapacityOrders: &orders,
		Items: []BatchItemIn{{ProductID: firstProduct(t, app, batch.BrandID), Price: 10000, Quota: &wide}},
	})
	if err != nil {
		t.Fatal(err)
	}
	item := firstItem(t, app, b.ID)
	const n = 8
	var wg sync.WaitGroup
	okN := 0
	var mu sync.Mutex
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := app.PlaceOrder(ctx, b.Code, PlaceOrder{
				Name: fmt.Sprintf("O%d", i), WhatsApp: fmt.Sprintf("08180000%04d", i), Method: "pickup",
				Items: []CartItem{{BatchItemID: item, Qty: 1}},
			})
			if err == nil {
				mu.Lock()
				okN++
				mu.Unlock()
			} else if code := errCode(err); code != "capacity" && code != "batch_closed" {
				errCh <- fmt.Errorf("goroutine %d: %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if okN != 3 {
		t.Fatalf("successes %d", okN)
	}
	var reserved int32
	if err := app.Pool.QueryRow(ctx, `SELECT reserved_orders FROM batch_capacities WHERE batch_id = $1`, b.ID).Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if reserved != 3 {
		t.Fatalf("reserved %d", reserved)
	}
}

func TestCapacityMinMaxCutoffAndFloor(t *testing.T) {
	app, user, batch := fixture(t)
	ctx := context.Background()
	minQty := int32(2)
	minRp := int64(31000)
	maxC := int32(3)
	wide := int32(30)
	b, err := app.CreateBatch(ctx, user.ID, batch.BrandID, BatchIn{
		Title: "Aturan", OpensAt: time.Now().Add(-time.Hour), ClosesAt: time.Now().Add(48 * time.Hour),
		ReadyAt: time.Now().Add(72 * time.Hour), AllowPickup: true, Publish: true, RulesSet: true,
		QuotaTotal: &wide, MinQty: &minQty, MinAmountRp: &minRp, MaxQtyPerCustomer: &maxC,
		Items: []BatchItemIn{{ProductID: firstProduct(t, app, batch.BrandID), Price: 15000, Quota: &wide}},
	})
	if err != nil {
		t.Fatal(err)
	}
	item := firstItem(t, app, b.ID)
	_, err = app.PlaceOrder(ctx, b.Code, PlaceOrder{
		Name: "Satu", WhatsApp: "081811110001", Method: "pickup",
		Items: []CartItem{{BatchItemID: item, Qty: 1}},
	})
	if errCode(err) != "below_min_qty" {
		t.Fatalf("min qty: %v", err)
	}
	_, err = app.PlaceOrder(ctx, b.Code, PlaceOrder{
		Name: "Dua", WhatsApp: "081811110002", Method: "pickup",
		Items: []CartItem{{BatchItemID: item, Qty: 2}},
	})
	if errCode(err) != "below_min_amount" {
		t.Fatalf("min rp: %v", err)
	}
	if _, err := app.PlaceOrder(ctx, b.Code, PlaceOrder{
		Name: "Tiga", WhatsApp: "081811110003", Method: "pickup",
		Items: []CartItem{{BatchItemID: item, Qty: 3}},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = app.PlaceOrder(ctx, b.Code, PlaceOrder{
		Name: "Tiga", WhatsApp: "081811110003", Method: "pickup",
		Items: []CartItem{{BatchItemID: item, Qty: 3}},
	})
	if errCode(err) != "above_max" {
		t.Fatalf("max: %v", err)
	}

	lead := int32(12 * 60)
	late, err := app.CreateBatch(ctx, user.ID, batch.BrandID, BatchIn{
		Title: "Mepet", OpensAt: time.Now().Add(-2 * time.Hour), ClosesAt: time.Now().Add(3 * time.Hour),
		ReadyAt: time.Now().Add(3 * time.Hour), AllowPickup: true, Publish: true, RulesSet: true,
		CutoffLeadMinutes: &lead,
		Items:             []BatchItemIn{{ProductID: firstProduct(t, app, batch.BrandID), Price: 10000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.PlaceOrder(ctx, late.Code, PlaceOrder{
		Name: "Telat", WhatsApp: "081811110009", Method: "pickup",
		Items: []CartItem{{BatchItemID: firstItem(t, app, late.ID), Qty: 1}},
	})
	if errCode(err) != "cutoff" {
		t.Fatalf("cutoff: %v", err)
	}

	one := int32(1)
	_, err = app.AdjustBatchCapacity(ctx, user.ID, b.ID, BatchCapacityInput{CapacityInput: CapacityInput{CapacityQty: &one}})
	if errCode(err) != "capacity_floor" {
		t.Fatalf("floor: %v", err)
	}
	ten := int32(10)
	if _, err := app.AdjustBatchCapacity(ctx, user.ID, b.ID, BatchCapacityInput{CapacityInput: CapacityInput{CapacityQty: &ten, MaxQtyPerCustomer: &maxC}}); err != nil {
		t.Fatal(err)
	}
}

func TestReservationReleasedOnlyWithoutProof(t *testing.T) {
	app, user, batch := fixture(t)
	ctx := context.Background()
	item := firstItem(t, app, batch.ID)
	pending := mustOrder(t, app, batch.Code, "Belum", "081822220001")
	kept := mustOrder(t, app, batch.Code, "Sudah", "081822220002")
	if _, err := app.Q.MarkOrderProof(ctx, kept.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Pool.Exec(ctx, `UPDATE orders SET expires_at = now() - interval '2 minutes' WHERE id = $1 OR id = $2`, pending.ID, kept.ID); err != nil {
		t.Fatal(err)
	}
	app.Tick(ctx)
	_ = user
	st, err := app.Q.GetOrder(ctx, pending.ID)
	if err != nil || st.PaymentStatus != "expired" {
		t.Fatalf("pending status %s %v", st.PaymentStatus, err)
	}
	st, err = app.Q.GetOrder(ctx, kept.ID)
	if err != nil || st.PaymentStatus != "proof_submitted" {
		t.Fatalf("proof status %s %v", st.PaymentStatus, err)
	}
	var sold int32
	if err := app.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(sold_count),0)::int FROM batch_items WHERE id = $1`, item).Scan(&sold); err != nil {
		t.Fatal(err)
	}
	if sold != 1 {
		t.Fatalf("sold %d, proof reservation was released", sold)
	}
}

func TestMinQuotaCancelMarksRefund(t *testing.T) {
	app, user, batch := fixture(t)
	ctx := context.Background()
	minP := int32(4)
	wide := int32(20)
	b, err := app.CreateBatch(ctx, user.ID, batch.BrandID, BatchIn{
		Title: "Kurang", OpensAt: time.Now().Add(-2 * time.Hour), ClosesAt: time.Now().Add(24 * time.Hour),
		ReadyAt: time.Now().Add(48 * time.Hour), AllowPickup: true, Publish: true, RulesSet: true,
		QuotaTotal: &wide, MinProceedQty: &minP,
		Items: []BatchItemIn{{ProductID: firstProduct(t, app, batch.BrandID), Price: 12000, Quota: &wide}},
	})
	if err != nil {
		t.Fatal(err)
	}
	o := mustOrder(t, app, b.Code, "Hampir", "081833330001")
	if _, err := app.Q.MarkOrderProof(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Pool.Exec(ctx, `UPDATE po_batches SET closes_at = now() - interval '1 minute' WHERE id = $1`, b.ID); err != nil {
		t.Fatal(err)
	}
	app.Tick(ctx)
	var state string
	if err := app.Pool.QueryRow(ctx, `SELECT quota_state FROM batch_capacities WHERE batch_id = $1`, b.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "below_min" {
		t.Fatalf("state %s", state)
	}
	var notes int
	if err := app.Pool.QueryRow(ctx, `SELECT count(*) FROM customer_notices WHERE order_id = $1 AND kind = 'below_min'`, o.ID).Scan(&notes); err != nil {
		t.Fatal(err)
	}
	if notes != 1 {
		t.Fatalf("notices %d", notes)
	}
	if err := app.DecideMinimum(ctx, user.ID, b.ID, false); err != nil {
		t.Fatal(err)
	}
	st, err := app.Q.GetOrder(ctx, o.ID)
	if err != nil || st.PaymentStatus != "cancelled" {
		t.Fatalf("status %s %v", st.PaymentStatus, err)
	}
	var refunds int
	if err := app.Pool.QueryRow(ctx, `SELECT count(*) FROM order_refund_flags WHERE order_id = $1`, o.ID).Scan(&refunds); err != nil {
		t.Fatal(err)
	}
	if refunds != 1 {
		t.Fatal("refund not marked")
	}
	var sold int32
	if err := app.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(sold_count),0)::int FROM batch_items WHERE batch_id = $1`, b.ID).Scan(&sold); err != nil {
		t.Fatal(err)
	}
	if sold != 0 {
		t.Fatalf("sold %d", sold)
	}
	stats, err := app.CapacityStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var cancels int32
	for _, s := range stats {
		if s.StoreID == b.StoreID {
			cancels = s.Cancellations
		}
	}
	if cancels < 1 {
		t.Fatalf("cancellations %d", cancels)
	}
}

func TestWaitlistNotifiedWhenCapacityFrees(t *testing.T) {
	app, user, batch := fixture(t)
	ctx := context.Background()
	one := int32(1)
	yes := true
	b, err := app.CreateBatch(ctx, user.ID, batch.BrandID, BatchIn{
		Title: "Antri", OpensAt: time.Now().Add(-time.Hour), ClosesAt: time.Now().Add(48 * time.Hour),
		ReadyAt: time.Now().Add(72 * time.Hour), AllowPickup: true, Publish: true, RulesSet: true,
		QuotaTotal: &one, WaitlistEnabled: &yes,
		Items: []BatchItemIn{{ProductID: firstProduct(t, app, batch.BrandID), Price: 9000, Quota: &one}},
	})
	if err != nil {
		t.Fatal(err)
	}
	o := mustOrder(t, app, b.Code, "Pertama", "081844440001")
	cur, err := app.Q.GetBatch(ctx, b.ID)
	if err != nil || cur.Status != "closed" {
		t.Fatalf("status %s %v", cur.Status, err)
	}
	joined, err := app.JoinWaitlist(ctx, b.Code, WaitlistIn{Name: "Antre", WhatsApp: "081844440002", Qty: 1})
	if err != nil {
		t.Fatal(err)
	}
	if joined.Status != "waiting" {
		t.Fatalf("wait %s", joined.Status)
	}
	if _, err := app.Pool.Exec(ctx, `UPDATE orders SET expires_at = now() - interval '2 minutes' WHERE id = $1`, o.ID); err != nil {
		t.Fatal(err)
	}
	app.Tick(ctx)
	got, err := app.WaitlistStatus(ctx, joined.Token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "notified" {
		t.Fatalf("status %s %s", got.Status, got.Message)
	}
	cur, err = app.Q.GetBatch(ctx, b.ID)
	if err != nil || cur.Status != "open" {
		t.Fatalf("reopen %s %v", cur.Status, err)
	}
}

func TestStoreOverrideBeatsBrandDefault(t *testing.T) {
	app, user, batch := fixture(t)
	ctx := context.Background()
	min3 := int32(3)
	if err := app.SaveBrandCapacity(ctx, user.ID, batch.BrandID, CapacityInput{MinQty: &min3}); err != nil {
		t.Fatal(err)
	}
	stores, err := app.Q.ListStores(ctx, batch.BrandID)
	if err != nil || len(stores) == 0 {
		t.Fatal(err)
	}
	inherited, err := app.CreateBatch(ctx, user.ID, batch.BrandID, BatchIn{
		Title: "Ikut", OpensAt: time.Now().Add(-time.Hour), ClosesAt: time.Now().Add(48 * time.Hour),
		ReadyAt: time.Now().Add(72 * time.Hour), AllowPickup: true, Publish: true,
		Items: []BatchItemIn{{ProductID: firstProduct(t, app, batch.BrandID), Price: 8000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.PlaceOrder(ctx, inherited.Code, PlaceOrder{
		Name: "Kecil", WhatsApp: "081855550001", Method: "pickup",
		Items: []CartItem{{BatchItemID: firstItem(t, app, inherited.ID), Qty: 1}},
	})
	if errCode(err) != "below_min_qty" {
		t.Fatalf("brand default: %v", err)
	}
	one := int32(1)
	if err := app.SaveStoreCapacity(ctx, user.ID, stores[0].ID, CapacityInput{MinQty: &one}, false); err != nil {
		t.Fatal(err)
	}
	over, err := app.CreateBatch(ctx, user.ID, batch.BrandID, BatchIn{
		Title: "Toko", OpensAt: time.Now().Add(-time.Hour), ClosesAt: time.Now().Add(48 * time.Hour),
		ReadyAt: time.Now().Add(72 * time.Hour), AllowPickup: true, Publish: true,
		Items: []BatchItemIn{{ProductID: firstProduct(t, app, batch.BrandID), Price: 8000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.PlaceOrder(ctx, over.Code, PlaceOrder{
		Name: "Cukup", WhatsApp: "081855550002", Method: "pickup",
		Items: []CartItem{{BatchItemID: firstItem(t, app, over.ID), Qty: 1}},
	}); err != nil {
		t.Fatal(err)
	}
}

func errCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestSlotCapacity(t *testing.T) {
	app, user, batch := fixture(t)
	ctx := context.Background()
	capQty := int32(1)
	wide := int32(10)
	start := time.Now().Add(72 * time.Hour)
	b, err := app.CreateBatch(ctx, user.ID, batch.BrandID, BatchIn{
		Title: "Slot", OpensAt: time.Now().Add(-time.Hour), ClosesAt: time.Now().Add(48 * time.Hour),
		ReadyAt: time.Now().Add(48 * time.Hour), AllowPickup: true, Publish: true,
		QuotaTotal: &wide,
		PickupSlots: []Slot{{
			Start: start, End: start.Add(2 * time.Hour), CapacityQty: &capQty,
		}},
		Items: []BatchItemIn{{ProductID: firstProduct(t, app, batch.BrandID), Price: 7000, Quota: &wide}},
	})
	if err != nil {
		t.Fatal(err)
	}
	item := firstItem(t, app, b.ID)
	if _, err := app.PlaceOrder(ctx, b.Code, PlaceOrder{
		Name: "A", WhatsApp: "081866660001", Method: "pickup", SlotIndex: 0,
		Items: []CartItem{{BatchItemID: item, Qty: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = app.PlaceOrder(ctx, b.Code, PlaceOrder{
		Name: "B", WhatsApp: "081866660002", Method: "pickup", SlotIndex: 0,
		Items: []CartItem{{BatchItemID: item, Qty: 1}},
	})
	if code := errCode(err); code != "capacity" && code != "batch_closed" {
		t.Fatalf("slot: %v", err)
	}
	var reserved int32
	if err := app.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(reserved_qty),0)::int FROM pickup_capacities WHERE batch_id = $1`, b.ID).Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if reserved != 1 {
		t.Fatalf("slot reserved %d", reserved)
	}
}
