package deck

import (
	"regexp"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

// parseDirectiveYAML parses directive YAML the way Marpit does: with the
// failsafe schema, so every scalar is a string ("true", "2", "#eee" once
// quoted). It returns nil when the text is not a YAML mapping.
//
// With loose set, a line "key: value" whose key is a known directive gets its
// value quoted first, so "backgroundColor: #eee" means the colour rather than
// an empty value followed by a YAML comment. Marp Core turns this on.
func parseDirectiveYAML(text string, loose bool, looseKeys []string) map[string]any {
	if loose {
		text = convertLoose(text, looseKeys)
	}
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(text), &n); err != nil {
		return nil
	}
	if n.Kind != yaml.DocumentNode || len(n.Content) == 0 || n.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	m, _ := failsafe(n.Content[0]).(map[string]any)
	return m
}

// failsafe converts a node keeping every scalar as its string, except an
// empty plain scalar, which js-yaml reads as null even in its failsafe schema.
func failsafe(n *yaml.Node) any {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value == "" && n.Style == 0 {
			return nil
		}
		return n.Value
	case yaml.SequenceNode:
		s := make([]any, len(n.Content))
		for i, c := range n.Content {
			s[i] = failsafe(c)
		}
		return s
	case yaml.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		keys := make([]string, 0, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i].Value
			if _, dup := m[k]; !dup {
				keys = append(keys, k)
			}
			m[k] = failsafe(n.Content[i+1])
		}
		m[orderKey] = keys
		return m
	case yaml.AliasNode:
		return failsafe(n.Alias)
	case yaml.DocumentNode:
		if len(n.Content) > 0 {
			return failsafe(n.Content[0])
		}
	}
	return nil
}

// orderKey records a mapping's key order: Marpit applies directives in the
// order they are written, and Go maps have none.
const orderKey = "\x00order"

// orderedKeys returns m's keys in source order.
func orderedKeys(m map[string]any) []string {
	if keys, ok := m[orderKey].([]string); ok {
		return keys
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

const yamlSpecialChars = `["'{|>~&*`

var looseCache sync.Map // key list → *regexp.Regexp

// convertLoose is Marpit's convertLoose (directives/yaml.js).
func convertLoose(text string, keys []string) string {
	re := looseMatcher(keys)
	var b strings.Builder
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		m := re.FindStringSubmatch(line)
		if m == nil {
			b.WriteString(line + "\n")
			continue
		}
		prop, value := m[1], m[2]
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || strings.ContainsRune(yamlSpecialChars, rune(trimmed[0])) {
			b.WriteString(line + "\n")
			continue
		}
		spaces := value[:len(value)-len(strings.TrimLeft(value, " \t"))]
		b.WriteString(prop + spaces + `"` + strings.ReplaceAll(trimmed, `"`, `\"`) + `"` + "\n")
	}
	return strings.TrimSpace(b.String())
}

func looseMatcher(keys []string) *regexp.Regexp {
	id := strings.Join(keys, "\x00")
	if re, ok := looseCache.Load(id); ok {
		return re.(*regexp.Regexp)
	}
	var alts []string
	for _, k := range keys {
		q := "_?" + regexp.QuoteMeta(k)
		alts = append(alts, q, `"`+q+`"`, `'`+q+`'`)
	}
	re := regexp.MustCompile(`^((?:` + strings.Join(alts, "|") + `)\s*:)(.+)$`)
	looseCache.Store(id, re)
	return re
}
