package hours

import (
	"errors"
	"testing"
	"time"
)

func at(loc *time.Location, y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, loc)
}

func wk(days ...Day) []Day { return days }

func officeWeek() []Day {
	var days []Day
	for wd := 0; wd < 7; wd++ {
		d := Day{Weekday: wd, Closed: wd == 0 || wd == 6}
		if !d.Closed {
			d.Slots = []Slot{{Start: "07:00", End: "11:00"}, {Start: "16:00", End: "21:00"}}
		}
		days = append(days, d)
	}
	return days
}

func everyDay(slots ...Slot) []Day {
	var days []Day
	for wd := 0; wd < 7; wd++ {
		days = append(days, Day{Weekday: wd, Slots: slots})
	}
	return days
}

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestCompute(t *testing.T) {
	jkt := Location("Asia/Jakarta")
	mak := Location("Asia/Makassar")
	jyp := Location("Asia/Jayapura")

	office := officeWeek()
	tueMorning := at(jkt, 2026, 11, 10, 10, 40) // Tuesday
	if tueMorning.Weekday() != time.Tuesday {
		t.Fatalf("fixture weekday: %s", tueMorning.Weekday())
	}

	pauseEnd := at(jkt, 2026, 11, 10, 14, 0)
	openUntil := at(jkt, 2026, 11, 10, 14, 0)
	closeStarts := at(jkt, 2026, 11, 10, 10, 0)

	cases := []struct {
		name       string
		in         Input
		wantOpen   bool
		wantState  string
		wantBadge  string
		wantHint   string
		wantSource string
		wantClose  string // local HH:MM of NextCloseAt, empty if nil
		wantOpenAt string // local HH:MM of NextOpenAt, empty if nil
	}{
		{
			name:     "closing soon inside morning shift",
			in:       Input{Timezone: "Asia/Jakarta", Now: tueMorning, Configured: true, Days: office},
			wantOpen: true, wantState: "closing_soon", wantBadge: "Buka",
			wantHint: "Segera tutup · 11:00", wantSource: "weekly",
			wantClose: "11:00", wantOpenAt: "16:00",
		},
		{
			name:     "gap between shifts",
			in:       Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 11, 10, 11, 5), Configured: true, Days: office},
			wantOpen: false, wantState: "closed", wantBadge: "Buka pukul 16:00",
			wantSource: "weekly", wantOpenAt: "16:00",
		},
		{
			name:     "after close next morning",
			in:       Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 11, 10, 21, 30), Configured: true, Days: office},
			wantOpen: false, wantState: "closed", wantBadge: "Buka lagi Rabu 07:00",
			wantSource: "weekly", wantOpenAt: "07:00",
		},
		{
			name:     "weekend rolls to monday",
			in:       Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 11, 14, 12, 0), Configured: true, Days: office},
			wantOpen: false, wantState: "closed", wantBadge: "Buka lagi Senin 07:00",
			wantSource: "weekly", wantOpenAt: "07:00",
		},
		{
			name: "overnight still open",
			in: Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 11, 9, 23, 0), Configured: true,
				Days: []Day{{Weekday: 1, Slots: []Slot{{Start: "22:00", End: "02:00"}}}, {Weekday: 2, Closed: true}}},
			wantOpen: true, wantState: "open", wantBadge: "Buka", wantSource: "weekly",
			wantClose: "02:00",
		},
		{
			name: "overnight spill next morning",
			in: Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 11, 10, 1, 30), Configured: true,
				Days: []Day{{Weekday: 1, Slots: []Slot{{Start: "22:00", End: "02:00"}}}, {Weekday: 2, Closed: true}}},
			wantOpen: true, wantState: "closing_soon", wantBadge: "Buka",
			wantHint: "Segera tutup · 02:00", wantSource: "weekly", wantClose: "02:00",
		},
		{
			name: "overnight ends on the minute",
			in: Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 11, 10, 2, 0), Configured: true,
				Days: []Day{{Weekday: 1, Slots: []Slot{{Start: "22:00", End: "02:00"}}}, {Weekday: 2, Closed: true}}},
			wantOpen: false, wantState: "closed", wantBadge: "Buka lagi Senin 22:00",
			wantSource: "weekly", wantOpenAt: "22:00",
		},
		{
			name: "special date beats weekly",
			in: Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 12, 25, 10, 0), Configured: true, Days: office,
				Specials: []Special{{ID: "natal", From: date(2026, 12, 25), To: date(2026, 12, 25), Closed: true, Note: "Libur", Rank: 1}}},
			wantOpen: false, wantState: "closed", wantBadge: "Buka lagi Senin 07:00",
			wantHint: "Libur", wantSource: "special", wantOpenAt: "07:00",
		},
		{
			name: "special hours replace the week",
			in: Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 12, 24, 8, 0), Configured: true, Days: office,
				Specials: []Special{{ID: "malam", From: date(2026, 12, 24), To: date(2026, 12, 24), Slots: []Slot{{Start: "10:00", End: "12:00"}}, Rank: 1}}},
			wantOpen: false, wantState: "closed", wantBadge: "Buka pukul 10:00",
			wantSource: "special", wantOpenAt: "10:00",
		},
		{
			name: "inside special hours",
			in: Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 12, 24, 10, 30), Configured: true, Days: office,
				Specials: []Special{{ID: "malam", From: date(2026, 12, 24), To: date(2026, 12, 24), Slots: []Slot{{Start: "10:00", End: "12:00"}}, Rank: 1}}},
			wantOpen: true, wantState: "open", wantBadge: "Buka", wantSource: "special",
			wantClose: "12:00", wantOpenAt: "07:00",
		},
		{
			name: "holiday cuts yesterday overnight",
			in: Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 11, 10, 1, 0), Configured: true,
				Days:     []Day{{Weekday: 1, Slots: []Slot{{Start: "22:00", End: "02:00"}}}},
				Specials: []Special{{ID: "libur", From: date(2026, 11, 10), To: date(2026, 11, 10), Closed: true, Note: "Libur", Rank: 2}}},
			wantOpen: false, wantState: "closed", wantBadge: "Buka lagi Senin 22:00",
			wantHint: "Libur", wantSource: "special", wantOpenAt: "22:00",
		},
		{
			name: "store special beats brand special",
			in: Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 12, 25, 9, 30), Configured: true, Days: office,
				Specials: []Special{
					{ID: "brand", From: date(2026, 12, 25), To: date(2026, 12, 25), Closed: true, Note: "Brand", Rank: 1},
					{ID: "store", From: date(2026, 12, 25), To: date(2026, 12, 25), Note: "Toko", Rank: 2, Slots: []Slot{{Start: "09:00", End: "10:00"}}},
				}},
			wantOpen: true, wantState: "closing_soon", wantBadge: "Buka",
			wantHint: "Segera tutup · 10:00", wantSource: "special", wantClose: "10:00",
		},
		{
			name: "pause until 14:00 hides the open shift",
			in: Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 11, 10, 10, 0), Configured: true, Days: office,
				Override: &Override{Kind: "pause", Reason: "Habis", Starts: closeStarts, Ends: &pauseEnd}},
			wantOpen: false, wantState: "closed", wantBadge: "Buka pukul 16:00",
			wantHint: "Tutup sementara · Habis", wantSource: "override", wantOpenAt: "16:00",
		},
		{
			name: "pause has expired back to the schedule",
			in: Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 11, 10, 14, 0), Configured: true, Days: office,
				Override: &Override{Kind: "pause", Reason: "Habis", Starts: closeStarts, Ends: &pauseEnd}},
			wantOpen: false, wantState: "closed", wantBadge: "Buka pukul 16:00",
			wantSource: "weekly", wantOpenAt: "16:00",
		},
		{
			name: "close now expires at the next schedule change",
			in: Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 11, 10, 10, 30), Configured: true, Days: office,
				Override: &Override{Kind: "closed", Reason: "Hujan", Starts: closeStarts}},
			wantOpen: false, wantState: "closed", wantBadge: "Buka pukul 16:00",
			wantHint: "Tutup sementara · Hujan", wantSource: "override", wantOpenAt: "16:00",
		},
		{
			name: "nil-end close is gone once the schedule changes",
			in: Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 11, 10, 11, 0), Configured: true, Days: office,
				Override: &Override{Kind: "closed", Starts: closeStarts}},
			wantOpen: false, wantState: "closed", wantBadge: "Buka pukul 16:00",
			wantSource: "weekly", wantOpenAt: "16:00",
		},
		{
			name: "open now during the afternoon gap",
			in: Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 11, 10, 12, 30), Configured: true, Days: office,
				Override: &Override{Kind: "open", Starts: at(jkt, 2026, 11, 10, 12, 0), Ends: &openUntil}},
			wantOpen: true, wantState: "open", wantBadge: "Buka", wantSource: "override",
			wantClose: "14:00", wantOpenAt: "16:00",
		},
		{
			name: "open override closing soon",
			in: Input{Timezone: "Asia/Jakarta", Now: at(jkt, 2026, 11, 10, 13, 40), Configured: true, Days: office,
				Override: &Override{Kind: "open", Starts: at(jkt, 2026, 11, 10, 12, 0), Ends: &openUntil}},
			wantOpen: true, wantState: "closing_soon", wantBadge: "Buka",
			wantHint: "Segera tutup · 14:00", wantSource: "override",
			wantClose: "14:00", wantOpenAt: "16:00",
		},
		{
			name: "wita is open while wib is still closed",
			in: Input{Timezone: "Asia/Makassar", Now: time.Date(2026, 11, 9, 23, 30, 0, 0, time.UTC), Configured: true,
				Days: everyDay(Slot{Start: "07:00", End: "11:00"})},
			wantOpen: true, wantState: "open", wantBadge: "Buka", wantSource: "weekly", wantClose: "11:00",
		},
		{
			name: "wib still closed at the same instant",
			in: Input{Timezone: "Asia/Jakarta", Now: time.Date(2026, 11, 9, 23, 30, 0, 0, time.UTC), Configured: true,
				Days: everyDay(Slot{Start: "07:00", End: "11:00"})},
			wantOpen: false, wantState: "closed", wantBadge: "Buka pukul 07:00", wantSource: "weekly", wantOpenAt: "07:00",
		},
		{
			name: "wit is later in the same shift",
			in: Input{Timezone: "Asia/Jayapura", Now: time.Date(2026, 11, 9, 23, 30, 0, 0, time.UTC), Configured: true,
				Days: everyDay(Slot{Start: "07:00", End: "11:00"})},
			wantOpen: true, wantState: "open", wantBadge: "Buka", wantSource: "weekly", wantClose: "11:00",
		},
		{
			name:     "legacy open without a schedule",
			in:       Input{Timezone: "Asia/Jakarta", Now: tueMorning, LegacyOpen: true},
			wantOpen: true, wantState: "open", wantBadge: "Buka", wantSource: "legacy",
		},
		{
			name:     "legacy closed without a schedule",
			in:       Input{Timezone: "Asia/Jakarta", Now: tueMorning, LegacyOpen: false},
			wantOpen: false, wantState: "closed", wantBadge: "Tutup", wantSource: "legacy",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Compute(tc.in)
			if got.Open != tc.wantOpen || got.State != tc.wantState || got.Badge != tc.wantBadge || got.Hint != tc.wantHint || got.Source != tc.wantSource {
				t.Fatalf("open=%v state=%s badge=%q hint=%q source=%s", got.Open, got.State, got.Badge, got.Hint, got.Source)
			}
			if tc.wantClose == "" && got.NextCloseAt != nil {
				t.Fatalf("next close %s", got.NextCloseAt.In(Location(tc.in.Timezone)))
			}
			if tc.wantClose != "" {
				if got.NextCloseAt == nil || clockOf(got.NextCloseAt.In(Location(tc.in.Timezone))) != tc.wantClose {
					t.Fatalf("next close %v want %s", got.NextCloseAt, tc.wantClose)
				}
			}
			if tc.wantOpenAt == "" && got.NextOpenAt != nil && tc.wantSource != "weekly" && tc.name == "overnight still open" {
				t.Fatalf("next open %s", got.NextOpenAt)
			}
			if tc.wantOpenAt != "" {
				if got.NextOpenAt == nil || clockOf(got.NextOpenAt.In(Location(tc.in.Timezone))) != tc.wantOpenAt {
					t.Fatalf("next open %v want %s", got.NextOpenAt, tc.wantOpenAt)
				}
			}
			if got.Zone == "" {
				t.Fatal("missing zone")
			}
		})
	}

	_ = mak
	_ = jyp
}

func TestNoDST(t *testing.T) {
	zones := []struct {
		tz   string
		want int
	}{
		{"Asia/Jakarta", 7 * 3600},
		{"WIB", 7 * 3600},
		{"Asia/Makassar", 8 * 3600},
		{"WITA", 8 * 3600},
		{"Asia/Jayapura", 9 * 3600},
		{"WIT", 9 * 3600},
	}
	stamps := []time.Time{
		time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC), // US spring-forward day
		time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC), // US fall-back day
	}
	days := everyDay(Slot{Start: "07:00", End: "11:00"})
	for _, z := range zones {
		loc := Location(z.tz)
		for _, stamp := range stamps {
			local := stamp.In(loc)
			_, off := local.Zone()
			if off != z.want {
				t.Fatalf("%s %s offset %d", z.tz, local, off)
			}
			// 10:00 local is inside 07:00–11:00 on every one of these dates.
			wall := time.Date(local.Year(), local.Month(), local.Day(), 10, 0, 0, 0, loc)
			got := Compute(Input{Timezone: z.tz, Now: wall, Configured: true, Days: days})
			if !got.Open || got.Source != "weekly" {
				t.Fatalf("%s %s open=%v source=%s badge=%s", z.tz, wall, got.Open, got.Source, got.Badge)
			}
			_, off2 := wall.Zone()
			if off2 != z.want {
				t.Fatalf("wall offset %d", off2)
			}
		}
	}
	if _, _, err := CanonicalZone("Europe/Amsterdam"); !errors.Is(err, ErrZone) {
		t.Fatal(err)
	}
}

func TestValidate(t *testing.T) {
	okDays := officeWeek()
	if err := ValidateWeek(okDays); err != nil {
		t.Fatal(err)
	}
	touching := []Day{{Weekday: 1, Slots: []Slot{{Start: "07:00", End: "11:00"}, {Start: "11:00", End: "12:00"}}}}
	if err := ValidateWeek(touching); err != nil {
		t.Fatal(err)
	}
	full := []Day{{Weekday: 1, Slots: []Slot{{Start: "00:00", End: "24:00"}}}}
	if err := ValidateWeek(full); err != nil {
		t.Fatal(err)
	}

	bad := []struct {
		name string
		days []Day
		spec bool
	}{
		{"overlap", []Day{{Weekday: 1, Slots: []Slot{{Start: "07:00", End: "11:00"}, {Start: "10:00", End: "12:00"}}}}, false},
		{"two overnights", []Day{{Weekday: 1, Slots: []Slot{{Start: "22:00", End: "02:00"}, {Start: "23:00", End: "03:00"}}}}, false},
		{"cross midnight", wk(
			Day{Weekday: 1, Slots: []Slot{{Start: "22:00", End: "03:00"}}},
			Day{Weekday: 2, Slots: []Slot{{Start: "02:00", End: "06:00"}}},
		), false},
		{"zero", []Day{{Weekday: 1, Slots: []Slot{{Start: "07:00", End: "07:00"}}}}, false},
		{"bad text", []Day{{Weekday: 1, Slots: []Slot{{Start: "7am", End: "11:00"}}}}, false},
		{"open without slots", []Day{{Weekday: 1, Closed: false}}, false},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateWeek(tc.days); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	overnightOK := wk(
		Day{Weekday: 1, Slots: []Slot{{Start: "22:00", End: "02:00"}}},
		Day{Weekday: 2, Slots: []Slot{{Start: "16:00", End: "21:00"}}},
	)
	if err := ValidateWeek(overnightOK); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSpecial(date(2026, 12, 24), date(2026, 12, 26), false, []Slot{{Start: "20:00", End: "10:00"}, {Start: "08:00", End: "09:00"}}); err == nil {
		t.Fatal("multi-day overnight should overlap itself")
	}
	if err := ValidateSpecial(date(2026, 12, 25), date(2026, 12, 24), true, nil); !errors.Is(err, ErrRange) {
		t.Fatal(err)
	}
	if err := ValidateSpecial(date(2026, 12, 25), date(2026, 12, 25), true, nil); err != nil {
		t.Fatal(err)
	}
}

func TestAllowOrder(t *testing.T) {
	jkt := Location("Asia/Jakarta")
	now := at(jkt, 2026, 11, 10, 22, 0) // Tuesday night, store closed
	today := at(jkt, 2026, 11, 10, 18, 0)
	tomorrow := at(jkt, 2026, 11, 11, 9, 0)
	// 10 Nov 20:00 WIB is 10 Nov 22:00 WIT. A pickup at 11 Nov 00:30 WIT is still 10 Nov in Jakarta.
	nowWIB := at(jkt, 2026, 11, 10, 20, 0)
	pickupWITDate := at(Location("Asia/Jayapura"), 2026, 11, 11, 0, 30)

	cases := []struct {
		name    string
		open    bool
		accept  bool
		pickup  *time.Time
		now     time.Time
		tz      string
		wantOK  bool
		wantWhy string
	}{
		{"open ignores the date", true, false, &today, now, "Asia/Jakarta", true, ""},
		{"closed no preorder", false, false, &tomorrow, now, "Asia/Jakarta", false, "closed"},
		{"closed today is immediate", false, true, &today, now, "Asia/Jakarta", false, "immediate"},
		{"closed no slot is immediate", false, true, nil, now, "Asia/Jakarta", false, "immediate"},
		{"future pickup allowed", false, true, &tomorrow, now, "Asia/Jakarta", true, ""},
		{"wit date is already the next civil day", false, true, &pickupWITDate, nowWIB, "Asia/Jayapura", true, ""},
		{"same instant is still today in wib", false, true, &pickupWITDate, nowWIB, "Asia/Jakarta", false, "immediate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, why := AllowOrder(tc.open, tc.accept, tc.pickup, tc.now, tc.tz)
			if ok != tc.wantOK || why != tc.wantWhy {
				t.Fatalf("ok=%v why=%s", ok, why)
			}
		})
	}
	if !Ordering(false, true, []time.Time{today, tomorrow}, now, "Asia/Jakarta") {
		t.Fatal("a later slot should allow ordering")
	}
	if Ordering(false, true, []time.Time{today}, now, "Asia/Jakarta") {
		t.Fatal("today only")
	}
	if !Ordering(true, false, nil, now, "Asia/Jakarta") {
		t.Fatal("open store")
	}
}
