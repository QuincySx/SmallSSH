// Package design exposes the SmallPen tokens consumed by the native desktop.
package design

import (
	_ "embed"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

//go:embed smallssh.smallpen/tokens.css
var stylesheet string

// SmallPen stores base and appearance values as custom properties on :root.
// App layout and palette use a subset of those properties at their call sites.
var tokens = readTokens(stylesheet)

func readTokens(css string) map[string]map[string]string {
	comments := regexp.MustCompile(`(?s)/\*.*?\*/`)
	blocks := regexp.MustCompile(`:root\s*(?:\[\s*data-appearance\s*=\s*["'](light|dark)["']\s*\])?\s*\{([^}]*)\}`)
	properties := regexp.MustCompile(`(?:^|;)\s*--([a-z0-9-]+)\s*:\s*([^;]+)`)
	themes := make(map[string]map[string]string)
	for _, block := range blocks.FindAllStringSubmatch(comments.ReplaceAllString(css, ""), -1) {
		values := themes[block[1]]
		if values == nil {
			values = make(map[string]string)
			themes[block[1]] = values
		}
		for _, property := range properties.FindAllStringSubmatch(block[2], -1) {
			values[property[1]] = strings.TrimSpace(property[2])
		}
	}
	return themes
}

func value(appearance, name string) string {
	seen := make(map[string]bool)
	for !seen[name] {
		seen[name] = true
		result := tokens[appearance][name]
		if result == "" {
			result = tokens[""][name]
		}
		if result == "" {
			panic(fmt.Sprintf("SmallPen token --%s is missing (%s)", name, appearance))
		}
		if !strings.HasPrefix(result, "var(--") || !strings.HasSuffix(result, ")") {
			return result
		}
		name = strings.TrimSpace(result[len("var(--") : len(result)-1])
	}
	panic(fmt.Sprintf("SmallPen token --%s has a circular alias", name))
}

// Length returns the base length in logical pixels.
func Length(name string) float32 {
	text := value("", name)
	number, ok := strings.CutSuffix(text, "px")
	length, err := strconv.ParseFloat(number, 32)
	if !ok || err != nil || !(length >= 0 && length <= math.MaxFloat32) {
		panic(fmt.Sprintf("SmallPen token --%s needs a non-negative px length, got %q", name, text))
	}
	return float32(length)
}

// TypeSize returns the font size from a SmallPen typography token.
func TypeSize(name string) float32 {
	size, _ := typography(name)
	return size
}

// TypeLineHeight returns the token's single-line height in logical pixels.
func TypeLineHeight(name string) float32 {
	size, leading := typography(name)
	return size * leading
}

func typography(name string) (float32, float32) {
	fields := strings.Fields(value("", name))
	if len(fields) >= 2 {
		if size, lineHeight, ok := strings.Cut(fields[1], "px/"); ok {
			points, sizeErr := strconv.ParseFloat(size, 32)
			leading, leadingErr := strconv.ParseFloat(lineHeight, 32)
			if sizeErr == nil && leadingErr == nil && points > 0 && leading > 0 && points*leading <= math.MaxFloat32 {
				return float32(points), float32(leading)
			}
		}
	}
	panic(fmt.Sprintf("SmallPen token --%s needs a px font size and unitless line height", name))
}

// Color returns a hex paint from the requested appearance.
func Color(dark bool, name string) string {
	appearance := "light"
	if dark {
		appearance = "dark"
	}
	text := value(appearance, name)
	hex, _ := strings.CutPrefix(text, "#")
	if _, err := strconv.ParseUint(hex, 16, 32); len(hex) == len(text) || (len(hex) != 3 && len(hex) != 4 && len(hex) != 6 && len(hex) != 8) || err != nil {
		panic(fmt.Sprintf("SmallPen token --%s needs a hex color, got %q (%s)", name, text, appearance))
	}
	return text
}
