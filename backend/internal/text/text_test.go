package text

import "testing"

func TestSlug(t *testing.T) {
	if got := Slug("Dapur Senja!"); got != "dapur-senja" {
		t.Fatal(got)
	}
	if ok, _ := SlugOK("admin", false); ok {
		t.Fatal("reserved")
	}
	if ok, _ := SlugOK("po", true); ok {
		t.Fatal("store po")
	}
	if ok, _ := SlugOK("dapur-senja", false); !ok {
		t.Fatal("good slug")
	}
	got := Suggest("dapur", func(s string) bool { return s == "dapur-2" })
	if len(got) != 3 || got[0] != "dapur-3" {
		t.Fatal(got)
	}
}

func TestWhatsApp(t *testing.T) {
	for _, in := range []string{"0812 3456 7890", "+6281234567890", "6281234567890", "81234567890", "006281234567890"} {
		got, err := WhatsApp(in)
		if err != nil || got != "+6281234567890" {
			t.Fatalf("%s -> %s %v", in, got, err)
		}
	}
	if _, err := WhatsApp("123"); err == nil {
		t.Fatal("short")
	}
}

func TestRupiah(t *testing.T) {
	if Rupiah(75137) != "75.137" || Rupiah(1000) != "1.000" || Rupiah(0) != "0" || Rupiah(-100) != "-100" {
		t.Fatal(Rupiah(75137), Rupiah(1000), Rupiah(-100))
	}
}
