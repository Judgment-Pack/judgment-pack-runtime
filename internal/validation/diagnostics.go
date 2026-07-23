package validation

import (
	"sort"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

type diagnosticCollector struct {
	items     []result.Diagnostic
	seen      map[string]struct{}
	truncated bool
}

func newDiagnosticCollector() *diagnosticCollector {
	return &diagnosticCollector{
		items: []result.Diagnostic{},
		seen:  map[string]struct{}{},
	}
}

func (c *diagnosticCollector) add(diagnostic result.Diagnostic) bool {
	key := diagnostic.Code + "\x00" + diagnostic.InstancePath + "\x00" + diagnostic.Message
	if _, exists := c.seen[key]; exists {
		return true
	}
	if len(c.items) >= MaxDiagnostics {
		c.truncated = true
		return false
	}
	c.seen[key] = struct{}{}
	c.items = append(c.items, diagnostic)
	return true
}

func (c *diagnosticCollector) addError(code, layer, instancePath, message string) bool {
	return c.add(result.ErrorDiagnostic(code, layer, instancePath, message))
}

func (c *diagnosticCollector) full() bool {
	return len(c.items) >= MaxDiagnostics
}

func (c *diagnosticCollector) sorted() []result.Diagnostic {
	output := append([]result.Diagnostic(nil), c.items...)
	sort.SliceStable(output, func(i, j int) bool {
		if output[i].InstancePath != output[j].InstancePath {
			return output[i].InstancePath < output[j].InstancePath
		}
		return output[i].Code < output[j].Code
	})
	return output
}
