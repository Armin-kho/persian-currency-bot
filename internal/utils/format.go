
package utils

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

var persianDigits = map[rune]rune{
	'0': '۰',
	'1': '۱',
	'2': '۲',
	'3': '۳',
	'4': '۴',
	'5': '۵',
	'6': '۶',
	'7': '۷',
	'8': '۸',
	'9': '۹',
}

func ToPersianDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for _, r := range s {
		if pr, ok := persianDigits[r]; ok {
			b.WriteRune(pr)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func FormatNumber(value float64, unit string, digits string) string {
	var out string
	switch unit {
	case "usd":
		// Always show 2 decimals for USD values.
		out = "$ " + formatFloatWithCommas(value, 2)
	default:
		// Tomans and others are integer display.
		out = formatIntWithCommas(int64(math.Round(value)))
	}
	if digits == "fa" {
		out = ToPersianDigits(out)
	}
	return out
}

func formatIntWithCommas(n int64) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return sign + s
	}
	var b strings.Builder
	b.Grow(len(s) + len(s)/3)
	rem := len(s) % 3
	if rem == 0 {
		rem = 3
	}
	b.WriteString(sign)
	b.WriteString(s[:rem])
	for i := rem; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func formatFloatWithCommas(f float64, decimals int) string {
	sign := ""
	if f < 0 {
		sign = "-"
		f = -f
	}
	pow := math.Pow10(decimals)
	f = math.Round(f*pow) / pow
	s := fmt.Sprintf("%.*f", decimals, f)
	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]
	var frac string
	if len(parts) == 2 {
		frac = parts[1]
	}
	// Insert commas into intPart
	var b strings.Builder
	b.Grow(len(s) + len(s)/3 + 2)
	b.WriteString(sign)

	if len(intPart) <= 3 {
		b.WriteString(intPart)
	} else {
		rem := len(intPart) % 3
		if rem == 0 {
			rem = 3
		}
		b.WriteString(intPart[:rem])
		for i := rem; i < len(intPart); i += 3 {
			b.WriteByte(',')
			b.WriteString(intPart[i : i+3])
		}
	}

	if decimals > 0 {
		b.WriteByte('.')
		// Ensure correct utf-8 length growth (not strictly needed)
		if utf8.RuneCountInString(frac) < decimals {
			frac = frac + strings.Repeat("0", decimals-utf8.RuneCountInString(frac))
		}
		b.WriteString(frac)
	}
	return b.String()
}
