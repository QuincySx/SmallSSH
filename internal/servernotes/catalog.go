package servernotes

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"

	"sshclient/internal/monitor"
)

//go:embed catalog.json
var catalogData []byte

type application struct {
	name  string
	order int
}

type applicationRule struct {
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Aliases      []string `json:"aliases"`
	Source       string   `json:"source"`
	Order        int      `json:"order"`
	UnitPatterns []string `json:"unit_patterns,omitempty"`
	Instances    bool     `json:"instances,omitempty"`
	Images       []string `json:"images,omitempty"`
	Stacks       []string `json:"stacks,omitempty"`
	Excludes     []string `json:"excludes,omitempty"`
}

type catalogPattern struct {
	rule int
	re   *regexp.Regexp
}

type applicationCatalog struct {
	rules    []applicationRule
	aliases  map[string]int
	images   map[string]int
	excludes map[string]bool
	patterns []catalogPattern
}

var commonApplications = func() *applicationCatalog {
	c, err := parseCatalog(catalogData)
	if err != nil {
		panic(err) // The bundled, tested rule file is a build-time resource.
	}
	return c
}()

func parseCatalog(data []byte) (*applicationCatalog, error) {
	c := &applicationCatalog{aliases: map[string]int{}, images: map[string]int{}, excludes: map[string]bool{}}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&c.rules); err != nil {
		return nil, fmt.Errorf("application catalog: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("application catalog: unexpected trailing data")
	}
	if len(c.rules) == 0 || len(c.rules) > 512 {
		return nil, fmt.Errorf("application catalog: invalid definition count")
	}
	names := map[string]bool{}
	for i, r := range c.rules {
		source, err := url.Parse(r.Source)
		if r.Name == "" || names[r.Name] || clean(r.Name, 96) != r.Name || r.Category == "" || r.Order < 0 || r.Order > 5 || len(r.Aliases) == 0 || err != nil || source.Scheme != "https" || source.Host == "" || source.User != nil {
			return nil, fmt.Errorf("application catalog: invalid definition %q", r.Name)
		}
		names[r.Name] = true
		for _, alias := range r.Aliases {
			if alias == "" || alias != strings.ToLower(alias) || len(alias) > 96 || strings.ContainsAny(alias, "/@:\\ \t\r\n") || clean(alias, 96) != alias {
				return nil, fmt.Errorf("application catalog: invalid alias %q", alias)
			}
			if _, exists := c.aliases[alias]; exists {
				return nil, fmt.Errorf("application catalog: duplicate alias %q", alias)
			}
			c.aliases[alias] = i
		}
		for _, image := range r.Images {
			if image == "" || image != strings.ToLower(image) || !strings.Contains(image, "/") || strings.ContainsAny(image, "@ \t\r\n") {
				return nil, fmt.Errorf("application catalog: invalid image %q", image)
			}
			if _, exists := c.images[image]; exists {
				return nil, fmt.Errorf("application catalog: duplicate image %q", image)
			}
			c.images[image] = i
		}
		for _, pattern := range r.UnitPatterns {
			re, err := regexp.Compile(pattern)
			if err != nil || !strings.HasPrefix(pattern, "^") || !strings.HasSuffix(pattern, "$") {
				return nil, fmt.Errorf("application catalog: invalid unit pattern %q", pattern)
			}
			c.patterns = append(c.patterns, catalogPattern{i, re})
		}
		for _, excluded := range r.Excludes {
			c.excludes[strings.ToLower(excluded)] = true
		}
	}
	for excluded := range c.excludes {
		if _, exists := c.aliases[excluded]; exists {
			return nil, fmt.Errorf("application catalog: excluded alias %q is also an application", excluded)
		}
	}
	return c, nil
}

// Matching happens entirely on this client against metadata already collected.
// Unit patterns are anchored, compiled once, and never applied to image tags.
func identifyService(raw string) (application, bool) {
	if len(raw) > 256 || strings.ContainsAny(raw, "/ \t\r\n") || clean(raw, 256) != raw {
		return application{}, false
	}
	name := strings.TrimSuffix(raw, ".service")
	base, instance, _ := strings.Cut(name, "@")
	key := strings.ToLower(base)
	if commonApplications.excludes[key] {
		return application{}, false
	}
	index, found := commonApplications.aliases[key]
	if !found {
		for _, pattern := range commonApplications.patterns {
			indices := pattern.re.FindStringSubmatchIndex(key)
			if indices == nil {
				continue
			}
			index, found = pattern.rule, true
			if group := pattern.re.SubexpIndex("instance"); group > 0 && indices[group*2] >= 0 {
				instance = key[indices[group*2]:indices[group*2+1]]
				if len(base) == len(key) {
					instance = base[indices[group*2]:indices[group*2+1]]
				}
			}
			break
		}
	}
	if !found {
		return application{}, false
	}
	r := commonApplications.rules[index]
	label := r.Name
	if r.Instances && instance != "" {
		label += "（" + clean(instance, 60) + "）"
	}
	return application{label, r.Order}, true
}

func identifyContainer(container monitor.Container) (application, bool) {
	image := strings.ToLower(container.Image)
	if len(image) > 512 || strings.ContainsAny(image, " \t\r\n") || clean(image, 512) != image {
		return application{}, false
	}
	repository, _, _ := strings.Cut(image, "@")
	if colon := strings.LastIndex(repository, ":"); colon > strings.LastIndex(repository, "/") {
		repository = repository[:colon]
	}
	index, found := commonApplications.images[repository]
	if !found {
		index, found = commonApplications.aliases[path.Base(repository)]
	}
	if !found {
		index, found = commonApplications.aliases[strings.ToLower(container.Names)]
	}
	if !found {
		return application{}, false
	}
	r := commonApplications.rules[index]
	label := r.Name
	if r.Instances && container.Names != "" && !strings.EqualFold(container.Names, r.Name) {
		label += "（" + clean(container.Names, 60) + "）"
	}
	return application{label, r.Order}, true
}
