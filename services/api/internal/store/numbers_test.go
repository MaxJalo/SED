package store

import "testing"

func TestFormatRegNumber(t *testing.T) {
	org := "ORG-01"
	cases := []struct {
		typeCode string
		subtype  string
		seq      int
		want     string
	}{
		{"входящий", "", 123, "НПО-ВХ-0123/26"},
		{"исходящий", "", 87, "НПО-ИСХ-0087/26"},
		{"приказ", "приказ_основной", 45, "НПО-П-045/26"},
		{"приказ", "приказ_лс", 112, "НПО-ЛС-112/26"},
		{"распоряжение", "", 19, "НПО-Р-019/26"},
		{"служебная_записка", "", 310, "НПО-СЗ-0310/26"},
		{"протокол", "", 27, "НПО-ПР-027/26"},
	}
	for _, tc := range cases {
		got := formatRegNumber(org, tc.typeCode, tc.subtype, 2026, tc.seq)
		if got != tc.want {
			t.Fatalf("%s/%s: got %s, want %s", tc.typeCode, tc.subtype, got, tc.want)
		}
	}
}
