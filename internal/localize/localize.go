// Package localize provides the app's messages through MyGo's official catalog.
package localize

import (
	"embed"
	"encoding/json"

	"github.com/egoist/mygo/i18n"
)

//go:embed en.json zh-Hans.json
var messages embed.FS

var catalog = newCatalog()
var sources = knownSources()

func newCatalog() *i18n.Catalog {
	c, err := i18n.NewCatalog(messages, "zh-Hans")
	if err != nil {
		panic(err)
	}
	i18n.Register(c)
	return c
}

func knownSources() map[string]string {
	data, err := messages.ReadFile("zh-Hans.json")
	if err != nil {
		panic(err)
	}
	var known map[string]string
	if err := json.Unmarshal(data, &known); err != nil {
		panic(err)
	}
	return known
}

// Text translates a catalog source in locale. Empty locale uses Simplified
// Chinese, and unknown sources pass through without entering MyGo's text cache.
// Callers format any Go printf arguments after translating the format string.
func Text(locale, source string) string {
	if _, ok := sources[source]; !ok {
		return source
	}
	return catalog.Format(locale, source, nil)
}

// Current translates a source in the language currently shown by MyGo.
func Current(source string) string { return Text(i18n.Locale(), source) }
