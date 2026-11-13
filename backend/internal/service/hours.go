package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/hours"
)

// StoreStatuses computes the effective open/closed state for each store.
// Nothing is cached and nothing is written: a manual override that has
// reached its end simply stops applying.
func (a *App) StoreStatuses(ctx context.Context, stores []db.Store, now time.Time) (map[uuid.UUID]hours.Status, error) {
	out := map[uuid.UUID]hours.Status{}
	if len(stores) == 0 {
		return out, nil
	}
	in, err := a.loadInputs(ctx, stores, now)
	if err != nil {
		return nil, err
	}
	for _, st := range stores {
		out[st.ID] = hours.Compute(in[st.ID])
	}
	return out, nil
}

func (a *App) StoreStatus(ctx context.Context, st db.Store, now time.Time) (hours.Status, error) {
	all, err := a.StoreStatuses(ctx, []db.Store{st}, now)
	if err != nil {
		return hours.Status{}, err
	}
	return all[st.ID], nil
}

type specialRow struct {
	ID      uuid.UUID
	BrandID uuid.UUID
	StoreID *uuid.UUID
	From    time.Time
	To      time.Time
	Closed  bool
	Slots   []hours.Slot
	Note    string
}

type overrideRow struct {
	Kind   string
	Reason string
	Starts time.Time
	Ends   *time.Time
}

func (a *App) loadInputs(ctx context.Context, stores []db.Store, now time.Time) (map[uuid.UUID]hours.Input, error) {
	storeIDs := make([]uuid.UUID, 0, len(stores))
	brandIDs := make([]uuid.UUID, 0, len(stores))
	seenBrand := map[uuid.UUID]struct{}{}
	for _, st := range stores {
		storeIDs = append(storeIDs, st.ID)
		if _, ok := seenBrand[st.BrandID]; !ok {
			brandIDs = append(brandIDs, st.BrandID)
			seenBrand[st.BrandID] = struct{}{}
		}
	}
	prefs := map[uuid.UUID]bool{}
	prows, err := a.Pool.Query(ctx, `SELECT store_id, inherit FROM store_hour_prefs WHERE store_id = ANY($1)`, storeIDs)
	if err != nil {
		return nil, err
	}
	for prows.Next() {
		var id uuid.UUID
		var inherit bool
		if err := prows.Scan(&id, &inherit); err != nil {
			prows.Close()
			return nil, err
		}
		prefs[id] = inherit
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		return nil, err
	}

	storeDays := map[uuid.UUID][]hours.Day{}
	if err := scanDays(ctx, a.Pool, `SELECT store_id, weekday, closed, slots FROM store_weekly_hours WHERE store_id = ANY($1)`, storeIDs, storeDays); err != nil {
		return nil, err
	}
	brandDays := map[uuid.UUID][]hours.Day{}
	if err := scanDays(ctx, a.Pool, `SELECT brand_id, weekday, closed, slots FROM brand_weekly_hours WHERE brand_id = ANY($1)`, brandIDs, brandDays); err != nil {
		return nil, err
	}

	srows, err := a.Pool.Query(ctx, `
		SELECT id, brand_id, store_id, date_from, date_to, closed, slots, note
		FROM special_hours WHERE brand_id = ANY($1)`, brandIDs)
	if err != nil {
		return nil, err
	}
	var specials []specialRow
	for srows.Next() {
		var row specialRow
		var store pgtype.UUID
		var raw []byte
		if err := srows.Scan(&row.ID, &row.BrandID, &store, &row.From, &row.To, &row.Closed, &raw, &row.Note); err != nil {
			srows.Close()
			return nil, err
		}
		if store.Valid {
			id := uuid.UUID(store.Bytes)
			row.StoreID = &id
		}
		row.From = civilDate(row.From)
		row.To = civilDate(row.To)
		row.Slots = decodeSlots(raw)
		specials = append(specials, row)
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return nil, err
	}

	orows, err := a.Pool.Query(ctx, `
		SELECT DISTINCT ON (store_id) store_id, kind, reason, starts_at, ends_at
		FROM store_overrides
		WHERE store_id = ANY($1) AND cleared_at IS NULL
		ORDER BY store_id, created_at DESC`, storeIDs)
	if err != nil {
		return nil, err
	}
	overrides := map[uuid.UUID]overrideRow{}
	for orows.Next() {
		var id uuid.UUID
		var row overrideRow
		if err := orows.Scan(&id, &row.Kind, &row.Reason, &row.Starts, &row.Ends); err != nil {
			orows.Close()
			return nil, err
		}
		overrides[id] = row
	}
	orows.Close()
	if err := orows.Err(); err != nil {
		return nil, err
	}

	out := make(map[uuid.UUID]hours.Input, len(stores))
	for _, st := range stores {
		inherit := true
		if v, ok := prefs[st.ID]; ok {
			inherit = v
		}
		days := brandDays[st.BrandID]
		configured := len(days) > 0
		if !inherit {
			days = storeDays[st.ID]
			configured = len(days) > 0
		}
		var specs []hours.Special
		for _, sp := range specials {
			if sp.BrandID != st.BrandID {
				continue
			}
			if sp.StoreID != nil && *sp.StoreID != st.ID {
				continue
			}
			rank := 1
			if sp.StoreID != nil {
				rank = 2
			}
			specs = append(specs, hours.Special{
				ID: sp.ID.String(), From: sp.From, To: sp.To, Closed: sp.Closed,
				Slots: sp.Slots, Note: sp.Note, Rank: rank,
			})
		}
		var ov *hours.Override
		if row, ok := overrides[st.ID]; ok {
			row := row
			ov = &hours.Override{Kind: row.Kind, Reason: row.Reason, Starts: row.Starts, Ends: row.Ends}
		}
		tz := st.Timezone
		if tz == "" {
			tz = "Asia/Jakarta"
		}
		out[st.ID] = hours.Input{
			Timezone: tz, Now: now, Configured: configured, LegacyOpen: st.IsOpen,
			Days: days, Specials: specs, Override: ov,
		}
	}
	return out, nil
}

func scanDays(ctx context.Context, pool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, sql string, ids []uuid.UUID, dest map[uuid.UUID][]hours.Day) error {
	rows, err := pool.Query(ctx, sql, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var wd int
		var closed bool
		var raw []byte
		if err := rows.Scan(&id, &wd, &closed, &raw); err != nil {
			return err
		}
		slots := decodeSlots(raw)
		if closed {
			slots = nil
		}
		dest[id] = append(dest[id], hours.Day{Weekday: wd, Closed: closed, Slots: slots})
	}
	return rows.Err()
}

func decodeSlots(raw []byte) []hours.Slot {
	if len(raw) == 0 {
		return nil
	}
	var slots []hours.Slot
	if err := json.Unmarshal(raw, &slots); err != nil {
		return nil
	}
	return slots
}

func encodeSlots(slots []hours.Slot) []byte {
	if slots == nil {
		slots = []hours.Slot{}
	}
	b, _ := json.Marshal(slots)
	return b
}

func civilDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func normalizeWeek(days []hours.Day) ([]hours.Day, error) {
	by := map[int]hours.Day{}
	for _, d := range days {
		if d.Closed {
			d.Slots = nil
		}
		by[d.Weekday] = d
	}
	out := make([]hours.Day, 0, 7)
	for wd := 0; wd < 7; wd++ {
		d, ok := by[wd]
		if !ok {
			d = hours.Day{Weekday: wd, Closed: true}
		}
		d.Weekday = wd
		out = append(out, d)
	}
	if err := hours.ValidateWeek(out); err != nil {
		return nil, E(400, "bad_hours", err.Error())
	}
	return out, nil
}

// HoursWrite is the seller's schedule form.
type HoursWrite struct {
	Inherit        *bool
	Timezone       string
	AcceptPreorder *bool
	Days           []hours.Day
}

func (a *App) SaveStoreHours(ctx context.Context, userID, storeID uuid.UUID, in HoursWrite) (db.Store, error) {
	st, err := a.ownedStore(ctx, userID, storeID)
	if err != nil {
		return db.Store{}, err
	}
	var iana string
	if strings.TrimSpace(in.Timezone) != "" {
		var err error
		iana, _, err = hours.CanonicalZone(in.Timezone)
		if err != nil {
			return db.Store{}, E(400, "bad_zone", err.Error())
		}
	}
	inherit := true
	err = a.Pool.QueryRow(ctx, `SELECT inherit FROM store_hour_prefs WHERE store_id = $1`, storeID).Scan(&inherit)
	if err != nil && err != pgx.ErrNoRows {
		return db.Store{}, err
	}
	if err == pgx.ErrNoRows {
		inherit = true
	}
	if in.Inherit != nil {
		inherit = *in.Inherit
	}
	var days []hours.Day
	if !inherit {
		days, err = normalizeWeek(in.Days)
		if err != nil {
			return db.Store{}, err
		}
	}
	_, acc, err := a.OwnedBrand(ctx, userID, st.BrandID)
	if err != nil {
		return db.Store{}, err
	}
	err = a.txConn(ctx, func(q *db.Queries, tx pgx.Tx) error {
		if iana != "" {
			if _, err := tx.Exec(ctx, `UPDATE stores SET timezone = $2 WHERE id = $1`, storeID, iana); err != nil {
				return err
			}
		}
		if in.AcceptPreorder != nil {
			if _, err := tx.Exec(ctx, `UPDATE stores SET accept_preorders_when_closed = $2 WHERE id = $1`, storeID, *in.AcceptPreorder); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO store_hour_prefs (store_id, inherit) VALUES ($1, $2)
			ON CONFLICT (store_id) DO UPDATE SET inherit = EXCLUDED.inherit, updated_at = now()`, storeID, inherit); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM store_weekly_hours WHERE store_id = $1`, storeID); err != nil {
			return err
		}
		if !inherit {
			for _, d := range days {
				if _, err := tx.Exec(ctx, `INSERT INTO store_weekly_hours (store_id, weekday, closed, slots) VALUES ($1, $2, $3, $4)`,
					storeID, d.Weekday, d.Closed, encodeSlots(d.Slots)); err != nil {
					return err
				}
			}
		}
		a.audit(ctx, q, &acc.ID, &userID, "store.hours", "store", storeID.String(), nil, map[string]any{"inherit": inherit})
		return nil
	})
	if err != nil {
		return db.Store{}, err
	}
	return a.Q.GetStore(ctx, storeID)
}

func (a *App) SaveBrandHours(ctx context.Context, userID, brandID uuid.UUID, days []hours.Day) error {
	_, acc, err := a.OwnedBrand(ctx, userID, brandID)
	if err != nil {
		return err
	}
	norm, err := normalizeWeek(days)
	if err != nil {
		return err
	}
	return a.txConn(ctx, func(q *db.Queries, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM brand_weekly_hours WHERE brand_id = $1`, brandID); err != nil {
			return err
		}
		for _, d := range norm {
			if _, err := tx.Exec(ctx, `INSERT INTO brand_weekly_hours (brand_id, weekday, closed, slots) VALUES ($1, $2, $3, $4)`,
				brandID, d.Weekday, d.Closed, encodeSlots(d.Slots)); err != nil {
				return err
			}
		}
		a.audit(ctx, q, &acc.ID, &userID, "brand.hours", "brand", brandID.String(), nil, map[string]any{"days": 7})
		return nil
	})
}

// SpecialWrite is a holiday or a date range with its own hours.
type SpecialWrite struct {
	DateFrom string
	DateTo   string
	Closed   bool
	Slots    []hours.Slot
	Note     string
}

func (a *App) AddStoreSpecial(ctx context.Context, userID, storeID uuid.UUID, in SpecialWrite) (uuid.UUID, error) {
	st, err := a.ownedStore(ctx, userID, storeID)
	if err != nil {
		return uuid.Nil, err
	}
	return a.insertSpecial(ctx, userID, st.BrandID, &storeID, in)
}

func (a *App) AddBrandSpecial(ctx context.Context, userID, brandID uuid.UUID, in SpecialWrite) (uuid.UUID, error) {
	if _, _, err := a.OwnedBrand(ctx, userID, brandID); err != nil {
		return uuid.Nil, err
	}
	return a.insertSpecial(ctx, userID, brandID, nil, in)
}

func (a *App) insertSpecial(ctx context.Context, userID, brandID uuid.UUID, storeID *uuid.UUID, in SpecialWrite) (uuid.UUID, error) {
	from, err := parseCivil(in.DateFrom)
	if err != nil {
		return uuid.Nil, err
	}
	toRaw := in.DateTo
	if strings.TrimSpace(toRaw) == "" {
		toRaw = in.DateFrom
	}
	to, err := parseCivil(toRaw)
	if err != nil {
		return uuid.Nil, err
	}
	note := strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(note) > 80 {
		return uuid.Nil, E(400, "bad_hours", "Catatan maksimal 80 huruf.")
	}
	slots := in.Slots
	if in.Closed {
		slots = nil
	}
	if err := hours.ValidateSpecial(from, to, in.Closed, slots); err != nil {
		return uuid.Nil, E(400, "bad_hours", err.Error())
	}
	if to.Sub(from) > 366*24*time.Hour {
		return uuid.Nil, E(400, "bad_date", "Rentang hari khusus maksimal setahun.")
	}
	var clash uuid.UUID
	q := `SELECT id FROM special_hours
		WHERE brand_id = $1 AND store_id IS NOT DISTINCT FROM $2
		  AND date_from <= $4::date AND date_to >= $3::date
		LIMIT 1`
	err = a.Pool.QueryRow(ctx, q, brandID, storeID, from.Format("2006-01-02"), to.Format("2006-01-02")).Scan(&clash)
	if err == nil {
		return uuid.Nil, E(409, "overlap", "Tanggal ini sudah punya jam khusus.")
	}
	if err != pgx.ErrNoRows {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = a.Pool.QueryRow(ctx, `
		INSERT INTO special_hours (brand_id, store_id, date_from, date_to, closed, slots, note)
		VALUES ($1, $2, $3::date, $4::date, $5, $6, $7) RETURNING id`,
		brandID, storeID, from.Format("2006-01-02"), to.Format("2006-01-02"), in.Closed, encodeSlots(slots), note).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	_, acc, _ := a.OwnedBrand(ctx, userID, brandID)
	_ = a.tx(ctx, func(q *db.Queries) error {
		a.audit(ctx, q, &acc.ID, &userID, "hours.special", "special", id.String(), nil, map[string]any{"note": note})
		return nil
	})
	return id, nil
}

func (a *App) DeleteSpecial(ctx context.Context, userID, specialID uuid.UUID) error {
	var brandID uuid.UUID
	err := a.Pool.QueryRow(ctx, `SELECT brand_id FROM special_hours WHERE id = $1`, specialID).Scan(&brandID)
	if err == pgx.ErrNoRows {
		return E(404, "not_found", "Hari khusus tidak ketemu.")
	}
	if err != nil {
		return err
	}
	if _, _, err := a.OwnedBrand(ctx, userID, brandID); err != nil {
		return err
	}
	_, err = a.Pool.Exec(ctx, `DELETE FROM special_hours WHERE id = $1`, specialID)
	return err
}

// OverrideWrite is Buka sekarang, Tutup sekarang, or Jeda sampai.
type OverrideWrite struct {
	Kind   string
	Reason string
	Until  string
}

func (a *App) SetOverride(ctx context.Context, userID, storeID uuid.UUID, in OverrideWrite) error {
	st, err := a.ownedStore(ctx, userID, storeID)
	if err != nil {
		return err
	}
	kind := strings.TrimSpace(in.Kind)
	if kind != "open" && kind != "closed" && kind != "pause" {
		return E(400, "bad_override", "Pilih buka, tutup, atau jeda.")
	}
	reason := strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(reason) > 40 {
		return E(400, "bad_override", "Alasan maksimal 40 huruf.")
	}
	var ends *time.Time
	if strings.TrimSpace(in.Until) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(in.Until))
		if err != nil {
			return E(400, "bad_override", "Waktu selesai belum pas.")
		}
		if !t.After(time.Now().Add(-time.Minute)) {
			return E(400, "bad_override", "Waktu selesai sudah lewat.")
		}
		ends = &t
	}
	if kind == "pause" && ends == nil {
		return E(400, "bad_override", "Jeda perlu waktu selesai.")
	}
	_, acc, err := a.OwnedBrand(ctx, userID, st.BrandID)
	if err != nil {
		return err
	}
	return a.txConn(ctx, func(q *db.Queries, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE store_overrides SET cleared_at = now() WHERE store_id = $1 AND cleared_at IS NULL`, storeID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO store_overrides (store_id, kind, reason, ends_at, created_by)
			VALUES ($1, $2, $3, $4, $5)`, storeID, kind, reason, ends, userID); err != nil {
			return err
		}
		a.audit(ctx, q, &acc.ID, &userID, "store.override", "store", storeID.String(), nil, map[string]any{"kind": kind, "reason": reason})
		return nil
	})
}

func (a *App) ClearOverride(ctx context.Context, userID, storeID uuid.UUID) error {
	if _, err := a.ownedStore(ctx, userID, storeID); err != nil {
		return err
	}
	_, err := a.Pool.Exec(ctx, `UPDATE store_overrides SET cleared_at = now() WHERE store_id = $1 AND cleared_at IS NULL`, storeID)
	return err
}

func (a *App) CopyStoreHours(ctx context.Context, userID, storeID uuid.UUID, targets []uuid.UUID) error {
	st, err := a.ownedStore(ctx, userID, storeID)
	if err != nil {
		return err
	}
	inputs, err := a.loadInputs(ctx, []db.Store{st}, time.Now())
	if err != nil {
		return err
	}
	in := inputs[st.ID]
	if !in.Configured {
		return E(400, "no_hours", "Toko ini belum punya jadwal untuk disalin.")
	}
	days, err := normalizeWeek(in.Days)
	if err != nil {
		return err
	}
	if len(targets) == 0 || len(targets) > 20 {
		return E(400, "bad_hours", "Pilih toko yang akan disalin.")
	}
	seen := map[uuid.UUID]struct{}{}
	for _, id := range targets {
		if id == storeID {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		other, err := a.Q.GetStore(ctx, id)
		if err != nil {
			return err
		}
		if other.BrandID != st.BrandID {
			return E(403, "forbidden", "Salin hanya ke toko di brand yang sama.")
		}
		if _, err := a.ownedStore(ctx, userID, id); err != nil {
			return err
		}
		err = a.txConn(ctx, func(q *db.Queries, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `DELETE FROM store_weekly_hours WHERE store_id = $1`, id); err != nil {
				return err
			}
			for _, d := range days {
				if _, err := tx.Exec(ctx, `INSERT INTO store_weekly_hours (store_id, weekday, closed, slots) VALUES ($1, $2, $3, $4)`,
					id, d.Weekday, d.Closed, encodeSlots(d.Slots)); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO store_hour_prefs (store_id, inherit) VALUES ($1, false)
				ON CONFLICT (store_id) DO UPDATE SET inherit = false, updated_at = now()`, id)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// HoursEditor is the seller schedule screen.
type HoursEditor struct {
	StoreID                   uuid.UUID     `json:"storeId"`
	BrandID                   uuid.UUID     `json:"brandId"`
	Timezone                  string        `json:"timezone"`
	Zone                      string        `json:"zone"`
	Inherit                   bool          `json:"inherit"`
	AcceptPreordersWhenClosed bool          `json:"acceptPreordersWhenClosed"`
	Days                      []hours.Day   `json:"days"`
	BrandDays                 []hours.Day   `json:"brandDays"`
	StoreDays                 []hours.Day   `json:"storeDays"`
	Specials                  []SpecialView `json:"specials"`
	Override                  *OverrideView `json:"override"`
	Status                    hours.Status  `json:"status"`
	Stores                    []StoreOption `json:"stores"`
}

type SpecialView struct {
	ID       uuid.UUID    `json:"id"`
	Scope    string       `json:"scope"`
	StoreID  *uuid.UUID   `json:"storeId"`
	DateFrom string       `json:"dateFrom"`
	DateTo   string       `json:"dateTo"`
	Closed   bool         `json:"closed"`
	Slots    []hours.Slot `json:"slots"`
	Note     string       `json:"note"`
}

type OverrideView struct {
	Kind     string     `json:"kind"`
	Reason   string     `json:"reason"`
	StartsAt time.Time  `json:"startsAt"`
	EndsAt   *time.Time `json:"endsAt"`
}

type StoreOption struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
}

func (a *App) HoursEditor(ctx context.Context, userID, storeID uuid.UUID, now time.Time) (HoursEditor, error) {
	st, err := a.ownedStore(ctx, userID, storeID)
	if err != nil {
		return HoursEditor{}, err
	}
	siblings, err := a.Q.ListStores(ctx, st.BrandID)
	if err != nil {
		return HoursEditor{}, err
	}
	inputs, err := a.loadInputs(ctx, siblings, now)
	if err != nil {
		return HoursEditor{}, err
	}
	in := inputs[st.ID]
	status := hours.Compute(in)
	inherit := true
	var pref bool
	err = a.Pool.QueryRow(ctx, `SELECT inherit FROM store_hour_prefs WHERE store_id = $1`, storeID).Scan(&pref)
	if err == nil {
		inherit = pref
	} else if err != pgx.ErrNoRows {
		return HoursEditor{}, err
	}
	storeDays := []hours.Day{}
	brandDays := []hours.Day{}
	if rows, err := a.daysOf(ctx, `SELECT weekday, closed, slots FROM store_weekly_hours WHERE store_id = $1 ORDER BY weekday`, storeID); err != nil {
		return HoursEditor{}, err
	} else {
		storeDays = rows
	}
	if rows, err := a.daysOf(ctx, `SELECT weekday, closed, slots FROM brand_weekly_hours WHERE brand_id = $1 ORDER BY weekday`, st.BrandID); err != nil {
		return HoursEditor{}, err
	} else {
		brandDays = rows
	}
	specs, err := a.specialViews(ctx, st.BrandID, &st.ID)
	if err != nil {
		return HoursEditor{}, err
	}
	var ov *OverrideView
	if status.Source == "override" {
		var row overrideRow
		err := a.Pool.QueryRow(ctx, `
			SELECT kind, reason, starts_at, ends_at FROM store_overrides
			WHERE store_id = $1 AND cleared_at IS NULL
			ORDER BY created_at DESC LIMIT 1`, storeID).Scan(&row.Kind, &row.Reason, &row.Starts, &row.Ends)
		if err == nil {
			ov = &OverrideView{Kind: row.Kind, Reason: row.Reason, StartsAt: row.Starts, EndsAt: row.Ends}
		} else if err != pgx.ErrNoRows {
			return HoursEditor{}, err
		}
	}
	opts := make([]StoreOption, 0, len(siblings))
	for _, s := range siblings {
		if s.ID == st.ID {
			continue
		}
		opts = append(opts, StoreOption{ID: s.ID, Name: s.Name, Slug: s.Slug})
	}
	if storeDays == nil {
		storeDays = []hours.Day{}
	}
	if brandDays == nil {
		brandDays = []hours.Day{}
	}
	if specs == nil {
		specs = []SpecialView{}
	}
	tz := st.Timezone
	if tz == "" {
		tz = "Asia/Jakarta"
	}
	return HoursEditor{
		StoreID: st.ID, BrandID: st.BrandID, Timezone: tz, Zone: hours.ZoneLabel(tz),
		Inherit: inherit, AcceptPreordersWhenClosed: st.AcceptPreordersWhenClosed,
		Days: in.Days, BrandDays: brandDays, StoreDays: storeDays,
		Specials: specs, Override: ov, Status: status, Stores: opts,
	}, nil
}

func (a *App) BrandHours(ctx context.Context, userID, brandID uuid.UUID) (map[string]any, error) {
	if _, _, err := a.OwnedBrand(ctx, userID, brandID); err != nil {
		return nil, err
	}
	days, err := a.daysOf(ctx, `SELECT weekday, closed, slots FROM brand_weekly_hours WHERE brand_id = $1 ORDER BY weekday`, brandID)
	if err != nil {
		return nil, err
	}
	if days == nil {
		days = []hours.Day{}
	}
	specs, err := a.specialViews(ctx, brandID, nil)
	if err != nil {
		return nil, err
	}
	brandOnly := []SpecialView{}
	for _, sp := range specs {
		if sp.Scope == "brand" {
			brandOnly = append(brandOnly, sp)
		}
	}
	return map[string]any{"brandId": brandID, "days": days, "specials": brandOnly}, nil
}

func (a *App) daysOf(ctx context.Context, sql string, id uuid.UUID) ([]hours.Day, error) {
	rows, err := a.Pool.Query(ctx, sql, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []hours.Day
	for rows.Next() {
		var wd int
		var closed bool
		var raw []byte
		if err := rows.Scan(&wd, &closed, &raw); err != nil {
			return nil, err
		}
		slots := decodeSlots(raw)
		if closed {
			slots = nil
		}
		if slots == nil {
			slots = []hours.Slot{}
		}
		out = append(out, hours.Day{Weekday: wd, Closed: closed, Slots: slots})
	}
	return out, rows.Err()
}

func (a *App) specialViews(ctx context.Context, brandID uuid.UUID, onlyStore *uuid.UUID) ([]SpecialView, error) {
	rows, err := a.Pool.Query(ctx, `
		SELECT id, store_id, date_from, date_to, closed, slots, note
		FROM special_hours WHERE brand_id = $1
		ORDER BY date_from, created_at`, brandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SpecialView
	for rows.Next() {
		var v SpecialView
		var store pgtype.UUID
		var from, to time.Time
		var raw []byte
		if err := rows.Scan(&v.ID, &store, &from, &to, &v.Closed, &raw, &v.Note); err != nil {
			return nil, err
		}
		v.DateFrom = civilDate(from).Format("2006-01-02")
		v.DateTo = civilDate(to).Format("2006-01-02")
		v.Slots = decodeSlots(raw)
		if v.Slots == nil {
			v.Slots = []hours.Slot{}
		}
		v.Scope = "brand"
		if store.Valid {
			id := uuid.UUID(store.Bytes)
			v.StoreID = &id
			v.Scope = "store"
		}
		if onlyStore != nil && v.Scope == "store" && *v.StoreID != *onlyStore {
			continue
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (a *App) ownedStore(ctx context.Context, userID, storeID uuid.UUID) (db.Store, error) {
	st, err := a.Q.GetStore(ctx, storeID)
	if err == pgx.ErrNoRows {
		return db.Store{}, E(404, "not_found", "Toko tidak ketemu.")
	}
	if err != nil {
		return db.Store{}, err
	}
	if _, _, err := a.OwnedBrand(ctx, userID, st.BrandID); err != nil {
		return db.Store{}, err
	}
	return st, nil
}

func parseCivil(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, E(400, "bad_date", "Tanggal belum pas.")
	}
	return t, nil
}

// AdminStore is one pin for the Office Pesenin store map.
type AdminStore struct {
	ID                        uuid.UUID    `json:"id"`
	BrandID                   uuid.UUID    `json:"brandId"`
	BrandName                 string       `json:"brandName"`
	BrandSlug                 string       `json:"brandSlug"`
	Name                      string       `json:"name"`
	Slug                      string       `json:"slug"`
	Address                   string       `json:"address"`
	MapsURL                   *string      `json:"mapsUrl"`
	Timezone                  string       `json:"timezone"`
	Zone                      string       `json:"zone"`
	AcceptPreordersWhenClosed bool         `json:"acceptPreordersWhenClosed"`
	IsOpen                    bool         `json:"isOpen"`
	EffectiveOpen             bool         `json:"effectiveOpen"`
	Status                    hours.Status `json:"status"`
}

func (a *App) AdminStores(ctx context.Context, now time.Time) ([]AdminStore, error) {
	rows, err := a.Pool.Query(ctx, `
		SELECT s.id, s.brand_id, s.name, s.slug, s.address, s.maps_url, s.timezone,
		       s.is_open, s.accept_preorders_when_closed, b.name, b.slug
		FROM stores s
		JOIN brands b ON b.id = s.brand_id
		WHERE s.archived_at IS NULL AND b.status <> 'archived'
		ORDER BY b.name, s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []AdminStore
	var stores []db.Store
	for rows.Next() {
		var row AdminStore
		var st db.Store
		if err := rows.Scan(&row.ID, &row.BrandID, &row.Name, &row.Slug, &row.Address, &row.MapsURL, &row.Timezone,
			&row.IsOpen, &row.AcceptPreordersWhenClosed, &row.BrandName, &row.BrandSlug); err != nil {
			return nil, err
		}
		st.ID = row.ID
		st.BrandID = row.BrandID
		st.Timezone = row.Timezone
		st.IsOpen = row.IsOpen
		st.AcceptPreordersWhenClosed = row.AcceptPreordersWhenClosed
		stores = append(stores, st)
		list = append(list, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if list == nil {
		list = []AdminStore{}
	}
	statuses, err := a.StoreStatuses(ctx, stores, now)
	if err != nil {
		return nil, err
	}
	for i := range list {
		st := statuses[list[i].ID]
		list[i].Status = st
		list[i].EffectiveOpen = st.Open
		list[i].Zone = st.Zone
		if list[i].Timezone == "" {
			list[i].Timezone = "Asia/Jakarta"
		}
	}
	return list, nil
}
