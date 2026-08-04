package exporter

import "testing"

func TestClassifyColor(t *testing.T) {
	cases := []struct {
		rgb       string
		wantValue float64
		wantSkip  bool
		wantRecog bool
	}{
		{"186,230,126", 1, false, true},
		{"239,107,115", 0, false, true},
		{"92,207,230", -1, false, true},
		{"253,109,20", 0.5, false, true},
		{"142,140,142", 0, true, false},
		{" 186 , 230 , 126 ", 1, false, true}, // whitespace tolerance
		{"1,2,3", -1, false, false},           // unrecognized
	}

	for _, c := range cases {
		got := ClassifyColor(c.rgb)
		if got.Value != c.wantValue || got.Skip != c.wantSkip || got.Recognized != c.wantRecog {
			t.Errorf("ClassifyColor(%q) = %+v, want value=%v skip=%v recognized=%v", c.rgb, got, c.wantValue, c.wantSkip, c.wantRecog)
		}
	}
}
