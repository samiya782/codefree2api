package proxy

import (
	"regexp"
	"strings"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func StripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

// ParseModelLines keeps lines whose first field starts with "codefree/".
func ParseModelLines(output string) []string {
	output = StripANSI(output)
	var models []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		id := fields[0]
		if !strings.HasPrefix(id, "codefree/") {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, id)
	}
	return models
}
