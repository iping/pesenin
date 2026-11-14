// Package hours computes a store's effective open/closed status.
//
// Precedence, evaluated at read time in the store's time zone, is
// manual override, then a special date, then the weekly schedule.
// Indonesia's zones do not observe DST; callers pass Asia/Jakarta (WIB),
// Asia/Makassar (WITA), or Asia/Jayapura (WIT).
package hours

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

const (
	closingSoonWindow = 30 * time.Minute
	horizon           = 21 * 24 * time.Hour
)

var (
	ErrBadTime  = errors.New("Jam harus seperti 07:00.")
	ErrOverlap  = errors.New("Jam bentrok. Atur supaya tidak saling tumpuk.")
	ErrNeedSlot = errors.New("Hari yang buka perlu minimal satu jam.")
	ErrZone     = errors.New("Zona waktu belum didukung. Pilih WIB, WITA, atau WIT.")
	ErrRange    = errors.New("Tanggal selesai harus sama atau setelah tanggal mulai.")
)

var dayName = []string{"Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}

// Slot is a wall-clock window. End <= start means the window crosses midnight
// (22:00–02:00). End may be 24:00, which is local midnight and does not cross.
type Slot struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// Day is one weekday. Weekday uses Go's numbering: 0 Sunday … 6 Saturday.
type Day struct {
	Weekday int    `json:"weekday"`
	Closed  bool   `json:"closed"`
	Slots   []Slot `json:"slots"`
}

// Special replaces the weekly schedule on each civil date in [From, To].
// Rank breaks ties: a store special (2) beats a brand special (1).
type Special struct {
	ID     string
	From   time.Time
	To     time.Time
	Closed bool
	Slots  []Slot
	Note   string
	Rank   int
}

// Override forces the store open or closed until Ends.
// A nil Ends means "until the next time the schedule itself would change".
// Kind pause is a close that always has an end.
type Override struct {
	Kind   string
	Reason string
	Starts time.Time
	Ends   *time.Time
}

// Input is everything Compute needs. Configured is false when the store
// still inherits an empty brand default and therefore follows LegacyOpen
// (the existing stores.is_open flag).
type Input struct {
	Timezone   string
	Now        time.Time
	Configured bool
	LegacyOpen bool
	Days       []Day
	Specials   []Special
	Override   *Override
}

// Status is the effective state at Input.Now.
type Status struct {
	Open            bool       `json:"open"`
	State           string     `json:"state"`
	ClosingSoon     bool       `json:"closingSoon"`
	Badge           string     `json:"badge"`
	Hint            string     `json:"hint"`
	Source          string     `json:"source"`
	Reason          string     `json:"reason"`
	Note            string     `json:"note"`
	Configured      bool       `json:"configured"`
	ScheduleOpen    bool       `json:"scheduleOpen"`
	Zone            string     `json:"zone"`
	Timezone        string     `json:"timezone"`
	NextOpenAt      *time.Time `json:"nextOpenAt"`
	NextCloseAt     *time.Time `json:"nextCloseAt"`
	NextChangeAt    *time.Time `json:"nextChangeAt"`
	NextOpenWeekday int        `json:"nextOpenWeekday"`
	NextOpenClock   string     `json:"nextOpenClock"`
	NextOpenSameDay bool       `json:"nextOpenSameDay"`
	CloseClock      string     `json:"closeClock"`
}

type span struct {
	start, end time.Time
	source     string
}

// CanonicalZone maps WIB/WITA/WIT and the three Indonesian IANA names.
// An empty input returns "", "", nil so callers can leave the stored zone alone.
func CanonicalZone(in string) (iana, label string, err error) {
	switch strings.ToUpper(strings.TrimSpace(in)) {
	case "":
		return "", "", nil
	case "WIB", "ASIA/JAKARTA":
		return "Asia/Jakarta", "WIB", nil
	case "WITA", "ASIA/MAKASSAR", "ASIA/UJUNG_PANDANG":
		return "Asia/Makassar", "WITA", nil
	case "WIT", "ASIA/JAYAPURA":
		return "Asia/Jayapura", "WIT", nil
	default:
		return "", "", ErrZone
	}
}

// Location loads the store zone. Unknown or empty values fall back to WIB.
func Location(tz string) *time.Location {
	iana, _, err := CanonicalZone(tz)
	if err != nil || iana == "" {
		if iana == "" && strings.TrimSpace(tz) != "" {
			if l, loadErr := time.LoadLocation(tz); loadErr == nil {
				return l
			}
		}
		iana = "Asia/Jakarta"
	}
	l, err := time.LoadLocation(iana)
	if err != nil {
		return time.FixedZone("WIB", 7*3600)
	}
	return l
}

func ZoneLabel(tz string) string {
	_, label, err := CanonicalZone(tz)
	if err != nil || label == "" {
		switch {
		case strings.Contains(tz, "Makassar"), strings.Contains(tz, "Ujung"):
			return "WITA"
		case strings.Contains(tz, "Jayapura"):
			return "WIT"
		default:
			return "WIB"
		}
	}
	return label
}

// ValidateWeek rejects overlapping slots, including an overnight slot that
// runs into the next weekday's slots. A closed day carries no slots.
func ValidateWeek(days []Day) error {
	by := map[int]Day{}
	for _, d := range days {
		if d.Weekday < 0 || d.Weekday > 6 {
			return fmt.Errorf("Hari tidak dikenal.")
		}
		if !d.Closed {
			if len(d.Slots) == 0 {
				return fmt.Errorf("%s: %w", dayName[d.Weekday], ErrNeedSlot)
			}
			if err := validateSlots(d.Slots); err != nil {
				return fmt.Errorf("%s: %w", dayName[d.Weekday], err)
			}
		}
		by[d.Weekday] = d
	}
	for wd := 0; wd < 7; wd++ {
		d := by[wd]
		if d.Closed {
			continue
		}
		next := by[(wd+1)%7]
		if next.Closed {
			continue
		}
		if overnightHits(d.Slots, next.Slots) {
			return fmt.Errorf("%s–%s: %w", dayName[wd], dayName[(wd+1)%7], ErrOverlap)
		}
	}
	return nil
}

// ValidateSpecial checks one date range. A multi-day range uses the same
// slots each day, so an overnight window must not collide with itself.
func ValidateSpecial(from, to time.Time, closed bool, slots []Slot) error {
	if to.Before(from) {
		return ErrRange
	}
	if closed {
		return nil
	}
	if len(slots) == 0 {
		return ErrNeedSlot
	}
	if err := validateSlots(slots); err != nil {
		return err
	}
	if civil(to).After(civil(from)) && overnightHits(slots, slots) {
		return ErrOverlap
	}
	return nil
}

func validateSlots(slots []Slot) error {
	if len(slots) > 8 {
		return errors.New("Maksimal 8 jam per hari.")
	}
	iv, err := intervals(slots)
	if err != nil {
		return err
	}
	sort.Slice(iv, func(i, j int) bool { return iv[i][0] < iv[j][0] })
	for i := 1; i < len(iv); i++ {
		if iv[i][0] < iv[i-1][1] {
			return ErrOverlap
		}
	}
	return nil
}

func overnightHits(today, next []Slot) bool {
	a, errA := intervals(today)
	b, errB := intervals(next)
	if errA != nil || errB != nil {
		return false
	}
	for _, iv := range a {
		if iv[1] <= 1440 {
			continue
		}
		spill := [2]int{0, iv[1] - 1440}
		for _, o := range b {
			if spill[0] < o[1] && o[0] < spill[1] {
				return true
			}
		}
	}
	return false
}

// intervals returns minutes from local midnight. An overnight end is > 1440.
func intervals(slots []Slot) ([][2]int, error) {
	out := make([][2]int, 0, len(slots))
	for _, s := range slots {
		a, err := parseHHMM(s.Start, false)
		if err != nil {
			return nil, err
		}
		b, err := parseHHMM(s.End, true)
		if err != nil {
			return nil, err
		}
		if a == b {
			return nil, ErrBadTime
		}
		if b < a {
			b += 1440
		}
		if b <= a || b-a > 1440 || (b-a == 1440 && !(a == 0 && b == 1440)) {
			return nil, ErrBadTime
		}
		out = append(out, [2]int{a, b})
	}
	return out, nil
}

func parseHHMM(s string, allow24 bool) (int, error) {
	s = strings.TrimSpace(s)
	if allow24 && s == "24:00" {
		return 1440, nil
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, ErrBadTime
	}
	h, errH := strconv.Atoi(parts[0])
	m, errM := strconv.Atoi(parts[1])
	if errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, ErrBadTime
	}
	return h*60 + m, nil
}

// AllowOrder decides whether a customer may place an order at now.
// A closed store blocks an immediate order (no pickup, or a pickup on today's
// local date). A future pickup date is allowed only when accept is true.
// The result code is "", "closed", or "immediate".
func AllowOrder(open, accept bool, pickup *time.Time, now time.Time, tz string) (bool, string) {
	if open {
		return true, ""
	}
	if pickup != nil && civilIn(*pickup, Location(tz)).After(civilIn(now, Location(tz))) {
		if accept {
			return true, ""
		}
		return false, "closed"
	}
	if !accept {
		return false, "closed"
	}
	return false, "immediate"
}

// Ordering is true when the store is open, or when it accepts pre-orders and
// at least one pickup instant falls on a later local date.
func Ordering(open, accept bool, pickups []time.Time, now time.Time, tz string) bool {
	if open {
		return true
	}
	for i := range pickups {
		if ok, _ := AllowOrder(false, accept, &pickups[i], now, tz); ok {
			return true
		}
	}
	return false
}

// Compute returns the effective status. It does not read the clock itself
// except when Input.Now is zero.
func Compute(in Input) Status {
	loc := Location(in.Timezone)
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	from := now.Add(-48 * time.Hour)
	if in.Override != nil && in.Override.Starts.Before(from) {
		from = in.Override.Starts.Add(-48 * time.Hour)
	}
	until := now.Add(horizon)
	if in.Override != nil && in.Override.Starts.Add(horizon).After(until) {
		until = in.Override.Starts.Add(horizon)
	}
	spans := collect(in, loc, from, until)
	merged := mergeSpans(spans)
	base := fromSchedule(in, loc, now, merged)
	if in.Override != nil && active(in.Override, merged, now) {
		return applyOverride(base, *in.Override, in, loc, now, merged)
	}
	return base
}

func collect(in Input, loc *time.Location, from, until time.Time) []span {
	start := midnight(from.In(loc))
	end := midnight(until.In(loc)).AddDate(0, 0, 1)
	var out []span
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		out = append(out, spansOn(day, in)...)
	}
	return out
}

func spansOn(day time.Time, in Input) []span {
	spec := winning(in.Specials, day)
	var slots []Slot
	source := ""
	specID := ""
	switch {
	case spec != nil && spec.Closed:
		return nil
	case spec != nil:
		slots = spec.Slots
		source = "special"
		specID = spec.ID
	case in.Configured:
		d := dayBy(in.Days, int(day.Weekday()))
		if d.Closed || len(d.Slots) == 0 {
			return nil
		}
		slots = d.Slots
		source = "weekly"
	default:
		return nil
	}
	iv, err := intervals(slots)
	if err != nil {
		return nil
	}
	nextMidnight := day.AddDate(0, 0, 1)
	nextSpec := winning(in.Specials, nextMidnight)
	clip := nextSpec != nil && nextSpec.ID != specID
	var out []span
	for _, pair := range iv {
		s := day.Add(time.Duration(pair[0]) * time.Minute)
		e := day.Add(time.Duration(pair[1]) * time.Minute)
		if clip && e.After(nextMidnight) {
			e = nextMidnight
		}
		if e.After(s) {
			out = append(out, span{start: s, end: e, source: source})
		}
	}
	return out
}

func dayBy(days []Day, wd int) Day {
	for _, d := range days {
		if d.Weekday == wd {
			return d
		}
	}
	return Day{Weekday: wd, Closed: true}
}

func winning(specs []Special, day time.Time) *Special {
	cur := civil(day)
	var best *Special
	for i := range specs {
		s := &specs[i]
		from, to := civil(s.From), civil(s.To)
		if cur.Before(from) || cur.After(to) {
			continue
		}
		if best == nil || outranks(s, best) {
			best = s
		}
	}
	return best
}

func outranks(a, b *Special) bool {
	if a.Rank != b.Rank {
		return a.Rank > b.Rank
	}
	alen := civil(a.To).Sub(civil(a.From))
	blen := civil(b.To).Sub(civil(b.From))
	if alen != blen {
		return alen < blen
	}
	return civil(a.From).After(civil(b.From))
}

func mergeSpans(in []span) []span {
	if len(in) == 0 {
		return nil
	}
	sorted := append([]span(nil), in...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].start.Equal(sorted[j].start) {
			return sorted[i].end.Before(sorted[j].end)
		}
		return sorted[i].start.Before(sorted[j].start)
	})
	out := []span{sorted[0]}
	for _, sp := range sorted[1:] {
		last := &out[len(out)-1]
		if !sp.start.After(last.end) {
			if sp.end.After(last.end) {
				last.end = sp.end
			}
			if sp.source == "special" {
				last.source = "special"
			}
			continue
		}
		out = append(out, sp)
	}
	return out
}

func covers(merged []span, t time.Time) bool {
	for _, sp := range merged {
		if !t.Before(sp.start) && t.Before(sp.end) {
			return true
		}
	}
	return false
}

func coverSpan(merged []span, t time.Time) *span {
	for i := range merged {
		sp := &merged[i]
		if !t.Before(sp.start) && t.Before(sp.end) {
			return sp
		}
	}
	return nil
}

func nextStartAfter(merged []span, t time.Time) *time.Time {
	for _, sp := range merged {
		if sp.start.After(t) {
			tt := sp.start
			return &tt
		}
	}
	return nil
}

// nextFlip is the next instant in (after, horizon) where schedule openness changes.
func nextFlip(merged []span, after, horizon time.Time) *time.Time {
	if !horizon.After(after) {
		return nil
	}
	open := covers(merged, after)
	var edges []time.Time
	seen := map[int64]struct{}{}
	for _, sp := range merged {
		for _, e := range []time.Time{sp.start, sp.end} {
			if !e.After(after) || !e.Before(horizon) {
				continue
			}
			key := e.UnixNano()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			edges = append(edges, e)
		}
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].Before(edges[j]) })
	for _, e := range edges {
		if covers(merged, e) != open {
			ee := e
			return &ee
		}
	}
	return nil
}

func fromSchedule(in Input, loc *time.Location, now time.Time, merged []span) Status {
	st := shell(in, loc)
	today := midnight(now.In(loc))
	spec := winning(in.Specials, today)
	switch {
	case spec != nil:
		st.Source = "special"
		st.Note = strings.TrimSpace(spec.Note)
	case in.Configured:
		st.Source = "weekly"
	default:
		st.Source = "legacy"
		st.Open = in.LegacyOpen
		st.ScheduleOpen = in.LegacyOpen
		if !in.LegacyOpen {
			if nxt := legacyNextOpen(in, loc, now); nxt != nil {
				st.NextOpenAt = nxt
			}
		}
		finish(&st, now, loc)
		return st
	}
	st.ScheduleOpen = covers(merged, now)
	st.Open = st.ScheduleOpen
	if sp := coverSpan(merged, now); sp != nil {
		end := sp.end
		st.NextCloseAt = &end
		st.NextOpenAt = nextStartAfter(merged, sp.end.Add(-time.Nanosecond))
		if st.NextOpenAt == nil {
			st.NextOpenAt = legacyNextOpen(in, loc, sp.end)
		}
		if sp.source == "special" {
			st.Source = "special"
		}
	} else {
		st.NextOpenAt = nextStartAfter(merged, now)
		if st.NextOpenAt == nil {
			st.NextOpenAt = legacyNextOpen(in, loc, now)
		}
	}
	finish(&st, now, loc)
	return st
}

func legacyNextOpen(in Input, loc *time.Location, after time.Time) *time.Time {
	if in.Configured || !in.LegacyOpen {
		return nil
	}
	day := midnight(after.In(loc))
	if !day.After(after) {
		day = day.AddDate(0, 0, 1)
	}
	for i := 0; i < 21; i++ {
		cur := day.AddDate(0, 0, i)
		if winning(in.Specials, cur) != nil {
			continue
		}
		tt := cur
		return &tt
	}
	return nil
}

func active(ov *Override, merged []span, now time.Time) bool {
	if ov == nil || now.Before(ov.Starts) {
		return false
	}
	end, ok := windowEnd(*ov, merged)
	if !ok {
		return true
	}
	return now.Before(end)
}

func windowEnd(ov Override, merged []span) (time.Time, bool) {
	if ov.Ends != nil {
		return *ov.Ends, true
	}
	flip := nextFlip(merged, ov.Starts, ov.Starts.Add(horizon))
	if flip == nil {
		return time.Time{}, false
	}
	return *flip, true
}

func applyOverride(base Status, ov Override, in Input, loc *time.Location, now time.Time, merged []span) Status {
	st := base
	st.Source = "override"
	st.Reason = strings.TrimSpace(ov.Reason)
	st.ScheduleOpen = base.ScheduleOpen
	end, hasEnd := windowEnd(ov, merged)
	forceClosed := ov.Kind == "closed" || ov.Kind == "pause"
	if !forceClosed {
		st.Open = true
		st.Note = ""
		if !hasEnd {
			st.NextCloseAt = nil
			st.NextOpenAt = nil
			st.ClosingSoon = false
		} else if sp := coverSpan(merged, end); sp != nil {
			closeAt := sp.end
			st.NextCloseAt = &closeAt
			st.NextOpenAt = nextStartAfter(merged, closeAt)
		} else {
			closeAt := end
			st.NextCloseAt = &closeAt
			st.NextOpenAt = nextStartAfter(merged, end)
			if st.NextOpenAt == nil {
				st.NextOpenAt = legacyNextOpen(in, loc, end)
			}
		}
		finish(&st, now, loc)
		return st
	}
	st.Open = false
	st.ClosingSoon = false
	st.NextCloseAt = nil
	if !hasEnd {
		st.NextOpenAt = nil
	} else if covers(merged, end) {
		tt := end
		st.NextOpenAt = &tt
	} else {
		st.NextOpenAt = nextStartAfter(merged, end)
		if st.NextOpenAt == nil {
			st.NextOpenAt = legacyNextOpen(in, loc, end)
		}
	}
	finish(&st, now, loc)
	return st
}

func shell(in Input, loc *time.Location) Status {
	tz := strings.TrimSpace(in.Timezone)
	if tz == "" {
		tz = "Asia/Jakarta"
	}
	return Status{
		Configured:      in.Configured,
		Zone:            ZoneLabel(tz),
		Timezone:        tz,
		NextOpenWeekday: -1,
	}
}

func finish(st *Status, now time.Time, loc *time.Location) {
	st.ClosingSoon = false
	if st.Open && st.NextCloseAt != nil {
		left := st.NextCloseAt.Sub(now)
		if left > 0 && left <= closingSoonWindow {
			st.ClosingSoon = true
		}
	}
	if st.Open {
		if st.ClosingSoon {
			st.State = "closing_soon"
		} else {
			st.State = "open"
		}
	} else {
		st.State = "closed"
	}
	localNow := now.In(loc)
	st.Badge = "Tutup"
	st.Hint = ""
	st.CloseClock = ""
	st.NextOpenClock = ""
	st.NextOpenSameDay = false
	st.NextOpenWeekday = -1
	if st.Open {
		st.Badge = "Buka"
		if st.NextCloseAt != nil {
			st.CloseClock = clockOf(st.NextCloseAt.In(loc))
			if st.ClosingSoon {
				st.Hint = "Segera tutup · " + st.CloseClock
			}
		}
	} else if st.NextOpenAt != nil {
		local := st.NextOpenAt.In(loc)
		st.NextOpenWeekday = int(local.Weekday())
		st.NextOpenClock = clockOf(local)
		st.NextOpenSameDay = sameDate(local, localNow)
		if st.NextOpenSameDay {
			st.Badge = "Buka pukul " + st.NextOpenClock
		} else {
			st.Badge = "Buka lagi " + dayName[local.Weekday()] + " " + st.NextOpenClock
		}
	}
	if !st.Open {
		if st.Source == "override" && st.Reason != "" {
			st.Hint = "Tutup sementara · " + st.Reason
		} else if st.Note != "" {
			st.Hint = st.Note
		}
	}
	if st.Open {
		st.NextChangeAt = st.NextCloseAt
	} else {
		st.NextChangeAt = st.NextOpenAt
	}
}

func clockOf(t time.Time) string {
	return fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())
}

func sameDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func midnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func civil(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func civilIn(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
