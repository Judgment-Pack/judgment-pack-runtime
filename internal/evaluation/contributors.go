package evaluation

import (
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// outcomeContributors reads only the completed walk. A true condition alone is
// insufficient: the entry must have supplied the final outcome. In particular,
// unresolved outcome values withhold attribution, and forcing exceptions skip
// all normal rules. The resolver already records each admitted rule/exception
// once with unique IDs within its namespace, so no membership is invented or
// discarded here. Sorting this new slice leaves the trace in document order.
func outcomeContributors(disposition result.Disposition, trace []result.TraceEntry) []result.Contributor {
	if disposition.Kind != "outcome" {
		return nil
	}
	var contributors []result.Contributor
	for _, entry := range trace {
		if entry.Condition != "true" || entry.Suppressed || entry.Skipped || entry.Outcome != disposition.OutcomeID {
			continue
		}
		switch entry.Stage {
		case "rule":
			contributors = append(contributors, result.Contributor{Kind: "rule", ID: entry.ID})
		case "exception":
			if entry.Effect == "force-outcome" {
				contributors = append(contributors, result.Contributor{Kind: "forcing-exception", ID: entry.ID})
			}
		}
	}
	// UTF-8 lexical order agrees with Unicode code-point order. Core localIds
	// are ASCII, and are retained without normalization or truncation.
	slices.SortFunc(contributors, func(a, b result.Contributor) int {
		if kind := strings.Compare(a.Kind, b.Kind); kind != 0 {
			return kind
		}
		return strings.Compare(a.ID, b.ID)
	})
	return contributors
}
