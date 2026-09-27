package units

import (
	"errors"
	"testing"
)

func TestParseTime(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"90", 90},
		{"1.5", 1.5},
		{"01:30", 90},
		{"1:02:03.250", 3723.25},
		{"90:00", 5400},
	}
	for _, c := range cases {
		got, err := ParseTime(c.in)
		if err != nil {
			t.Errorf("ParseTime(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseTime(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseTimeErrors(t *testing.T) {
	for _, in := range []string{"", "-1", "1:60", "1:2:3:4", "1:30.1234", "abc",
		"nan", "NaN", "inf", "-inf", "Inf", "+Inf"} {
		if _, err := ParseTime(in); err == nil {
			t.Errorf("ParseTime(%q) expected error, got nil", in)
		} else if !errors.Is(err, ErrSyntax) {
			t.Errorf("ParseTime(%q) error %v does not wrap ErrSyntax", in, err)
		}
	}
}

func TestFormatClock(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{12.4, "00:12"},
		{0, "00:00"},
		{599.9, "09:59"},
		{3725, "1:02:05"},
		{-5, "00:00"},
	}
	for _, c := range cases {
		if got := FormatClock(c.in); got != c.want {
			t.Errorf("FormatClock(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{2, "2"},
		{1.5, "1.5"},
		{0.3 / 0.5, "0.6"},
		{1.0 / 3.0, "0.333333"},
	}
	for _, c := range cases {
		if got := FormatNumber(c.in); got != c.want {
			t.Errorf("FormatNumber(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"20MB", 20_000_000},
		{"500KB", 500_000},
		{"1.5GB", 1_500_000_000},
		{"20M", 20_000_000},
		{"25MiB", 26_214_400},
		{"100B", 100},
	}
	for _, c := range cases {
		got, err := ParseSize(c.in)
		if err != nil {
			t.Errorf("ParseSize(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseSize(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseSizeErrors(t *testing.T) {
	for _, in := range []string{"20", "0MB", "-5MB", "20XB"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) expected error, got nil", in)
		} else if !errors.Is(err, ErrSyntax) {
			t.Errorf("ParseSize(%q) error %v does not wrap ErrSyntax", in, err)
		}
	}
}

func TestFormatSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{512, "512 B"},
		{500_000, "500 KB"},
		{6_200_000, "6.2 MB"},
		{276_100_000, "276.1 MB"},
		{1_250_000_000, "1.25 GB"},
	}
	for _, c := range cases {
		if got := FormatSize(c.in); got != c.want {
			t.Errorf("FormatSize(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseBitrateK(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"128k", 128},
		{"128K", 128},
	}
	for _, c := range cases {
		got, err := ParseBitrateK(c.in)
		if err != nil {
			t.Errorf("ParseBitrateK(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseBitrateK(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseBitrateKErrors(t *testing.T) {
	for _, in := range []string{"128", "31k", "513k", "k"} {
		if _, err := ParseBitrateK(in); err == nil {
			t.Errorf("ParseBitrateK(%q) expected error, got nil", in)
		} else if !errors.Is(err, ErrSyntax) {
			t.Errorf("ParseBitrateK(%q) error %v does not wrap ErrSyntax", in, err)
		}
	}
}

func TestParsePercent(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"50%", 50},
		{"50", 50},
		{"12.5%", 12.5},
	}
	for _, c := range cases {
		got, err := ParsePercent(c.in)
		if err != nil {
			t.Errorf("ParsePercent(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParsePercent(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParsePercentErrors(t *testing.T) {
	for _, in := range []string{"0%", "401%", "%"} {
		if _, err := ParsePercent(in); err == nil {
			t.Errorf("ParsePercent(%q) expected error, got nil", in)
		} else if !errors.Is(err, ErrSyntax) {
			t.Errorf("ParsePercent(%q) error %v does not wrap ErrSyntax", in, err)
		}
	}
}
