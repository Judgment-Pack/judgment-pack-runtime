package graph

import (
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// MaxEvaluationReportBytes bounds a Core 0.3 graph's retained evaluation report.
// Nodes, headline and route fan-out are charged before retention; the final
// envelope is checked before any audit write. Portable routes are never cut.
const MaxEvaluationReportBytes = MaxInputsBytes

func evaluationReportBudgetFailure() *evaluation.Failure {
	return &evaluation.Failure{Code: "JPS-GRAPH-REPORT-BUDGET", Message: "The Core 0.3 graph evaluation report exceeds its byte limit; no truncated disposition or audit record is produced.", ExitCode: result.ExitIO}
}

func graphHandoffs(node result.GraphNodeEvaluation) []result.GraphHandoff {
	if node.Disposition.Handoff.State != "requested" {
		return nil
	}
	if node.Disposition.Handoff.Routes == nil {
		return []result.GraphHandoff{{Node: node.Node, TriggeredBy: node.Disposition.Handoff.TriggeredBy, Target: node.HandoffTarget}}
	}
	handoffs := make([]result.GraphHandoff, 0, len(node.Disposition.Handoff.Routes))
	for _, route := range node.Disposition.Handoff.Routes {
		// Graph handoffs are display data; portable routes retain authored names.
		target := result.HandoffTarget{Kind: route.Kind, Name: display.Sanitize(route.Name)}
		handoffs = append(handoffs, result.GraphHandoff{Node: node.Node, TriggeredBy: node.Disposition.Handoff.TriggeredBy, Target: &target})
	}
	return handoffs
}
