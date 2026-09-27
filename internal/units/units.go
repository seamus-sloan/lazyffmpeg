// Package units parses and formats the time, size, bitrate and percent
// values accepted on the lazyff command line and in the TUI's step modals.
package units

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// ErrSyntax is wrapped by every parse error in this package.
var ErrSyntax = errors.New("invalid value")

func syntaxErr(s string) error {
	return fmt.Errorf("%w: %q", ErrSyntax, s)
}

var (
	uintRe    = regexp.MustCompile(`^\d+$`)
	secondsRe = regexp.MustCompile(`^\d+(\.\d{1,3})?$`)
)

// ParseTime parses a duration given as plain seconds ("90", "1.5") or as
// [hh:]mm:ss[.ms]. Minutes may exceed 59 when no hours component is present.
func ParseTime(s string) (float64, error) {
	if s == "" {
		return 0, syntaxErr(s)
	}
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		v, err := strconv.ParseFloat(parts[0], 64)
		if err != nil || v < 0 {
			return 0, syntaxErr(s)
		}
		return v, nil
	case 2:
		if !uintRe.MatchString(parts[0]) {
			return 0, syntaxErr(s)
		}
		mm, _ := strconv.ParseFloat(parts[0], 64)
		ss, err := parseSecondsPart(parts[1])
		if err != nil || ss >= 60 {
			return 0, syntaxErr(s)
		}
		return mm*60 + ss, nil
	case 3:
		if !uintRe.MatchString(parts[0]) || !uintRe.MatchString(parts[1]) {
			return 0, syntaxErr(s)
		}
		hh, _ := strconv.ParseFloat(parts[0], 64)
		mm, _ := strconv.ParseFloat(parts[1], 64)
		if mm >= 60 {
			return 0, syntaxErr(s)
		}
		ss, err := parseSecondsPart(parts[2])
		if err != nil || ss >= 60 {
			return 0, syntaxErr(s)
		}
		return hh*3600 + mm*60 + ss, nil
	default:
		return 0, syntaxErr(s)
	}
}

func parseSecondsPart(s string) (float64, error) {
	if !secondsRe.MatchString(s) {
		return 0, ErrSyntax
	}
	return strconv.ParseFloat(s, 64)
}

// FormatClock renders seconds as "mm:ss" below one hour, "h:mm:ss" from one
// hour up. Fractional seconds are floored; negative input renders "00:00".
func FormatClock(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	total := int64(math.Floor(sec))
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// FormatNumber renders x rounded to 6 decimal places in the shortest form
// that round-trips, e.g. 2 -> "2", 1.5 -> "1.5", 1/3 -> "0.333333".
func FormatNumber(x float64) string {
	return strconv.FormatFloat(math.Round(x*1e6)/1e6, 'f', -1, 64)
}

var sizeRe = regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)\s*([A-Za-z]+)$`)

// ParseSize parses a byte size such as "20MB", "500KB", "25MiB" (case
// insensitive, optional space before the unit). Decimal units (B, K/KB,
// M/MB, G/GB) use powers of 1000; binary units (KiB, MiB, GiB) use 1024.
func ParseSize(s string) (int64, error) {
	m := sizeRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, syntaxErr(s)
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, syntaxErr(s)
	}
	var mult float64
	switch strings.ToUpper(m[2]) {
	case "B":
		mult = 1
	case "K", "KB":
		mult = 1e3
	case "M", "MB":
		mult = 1e6
	case "G", "GB":
		mult = 1e9
	case "KIB":
		mult = 1024
	case "MIB":
		mult = 1024 * 1024
	case "GIB":
		mult = 1024 * 1024 * 1024
	default:
		return 0, syntaxErr(s)
	}
	val := n * mult
	if val <= 0 {
		return 0, syntaxErr(s)
	}
	return int64(math.Round(val)), nil
}

// FormatSize renders a byte count using decimal units: "N B" below 1000,
// "N KB" below 1e6 (0 dp), "N MB" below 1e9 (1 dp), else "N GB" (2 dp).
func FormatSize(n int64) string {
	switch {
	case n < 1_000:
		return fmt.Sprintf("%d B", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.0f KB", float64(n)/1e3)
	case n < 1_000_000_000:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	default:
		return fmt.Sprintf("%.2f GB", float64(n)/1e9)
	}
}

var bitrateRe = regexp.MustCompile(`(?i)^(\d+)k$`)

// ParseBitrateK parses an audio bitrate given as "<n>k", 32 <= n <= 512.
func ParseBitrateK(s string) (int, error) {
	m := bitrateRe.FindStringSubmatch(s)
	if m == nil {
		return 0, syntaxErr(s)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 32 || n > 512 {
		return 0, syntaxErr(s)
	}
	return n, nil
}

var percentRe = regexp.MustCompile(`^(\d+(?:\.\d+)?)%?$`)

// ParsePercent parses "<n>" or "<n>%", 0 < n <= 400.
func ParsePercent(s string) (float64, error) {
	m := percentRe.FindStringSubmatch(s)
	if m == nil {
		return 0, syntaxErr(s)
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil || n <= 0 || n > 400 {
		return 0, syntaxErr(s)
	}
	return n, nil
}
