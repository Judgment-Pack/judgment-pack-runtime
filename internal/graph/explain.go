package graph

import (
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Plan states one graph's evaluation plan without evaluating anything: the
// deterministic node order, and per node the pack it resolves to and every
// feed that will arrive before it runs. The one read it performs per node is
// the identity echo — the pack document's own id and version — and a document
// that cannot be read is reported on its step rather than failing the plan: a
// plan has to be printable while one pack is mid-edit, exactly as the project
// inventory has to be listable.
//
// The graph itself is held to its Structural checks first, completely: a plan
// for a graph whose own rules do not check out would narrate an order nothing
// will ever run in. The pack-reading checks stay with validate, which is what
// lets this plan keep its promise about unreadable packs.
func Plan(loaded *project.Project, doc Document, graphPath, command string) (result.GraphPlan, *evaluation.Failure) {
	if diagnostics := doc.Structural(loaded); len(diagnostics) > 0 {
		first := diagnostics[0]
		location := first.InstancePath
		if location == "" {
			location = "<root>"
		}
		return result.GraphPlan{}, &evaluation.Failure{
			Code:     "JPS-GRAPH-INVALID",
			Message:  fmt.Sprintf("The graph has no plan because its document and reference checks found %d problem(s); run experimental graph validate for the complete report. First diagnostic: %s at %s: %s", len(diagnostics), first.Code, display.Sanitize(location), display.Sanitize(first.Message)),
			ExitCode: result.ExitInvalid,
		}
	}
	order, _ := doc.Order()
	steps := make([]result.GraphPlanStep, 0, len(order))
	for index, nodeID := range order {
		packID := doc.Nodes[nodeID].Pack
		entry, _ := loaded.Entry(packID)
		step := result.GraphPlanStep{
			Order: index + 1,
			Node:  nodeID,
			Pack:  packID,
			Path:  entry.Path,
			Feeds: []result.GraphPlanFeed{},
		}
		step.PackID, step.PackVersion, step.Detail = packIdentity(loaded, entry)
		for _, edge := range doc.Edges {
			if edge.To != nodeID {
				continue
			}
			if edge.Fact != "" {
				step.Feeds = append(step.Feeds, result.GraphPlanFeed{From: edge.From, Fact: edge.Fact})
			}
			if edge.Evidence != nil {
				step.Feeds = append(step.Feeds, result.GraphPlanFeed{
					From:     edge.From,
					Evidence: edge.Evidence.ID,
					// The default is stated rather than implied: a plan whose reader
					// must know the default has not explained anything.
					OnUnresolved: edge.Evidence.State("unresolved"),
				})
			}
		}
		steps = append(steps, step)
	}
	return result.GraphPlan{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        "planned",
		Kind:          result.ProjectKind,
		FormatVersion: FormatVersion,
		ConfigPath:    loaded.ConfigPath,
		GraphPath:     graphPath,
		GraphID:       doc.ID,
		GraphVersion:  doc.Version,
		ResultNode:    doc.Result,
		Steps:         steps,
	}, nil
}

// packIdentity reads one configured pack's own id and version members for the
// plan's echo. A document that cannot be read or decoded leaves both empty
// and the reason in the detail, never a guess — the same posture the project
// inventory takes.
func packIdentity(loaded *project.Project, entry project.Pack) (id, version, detail string) {
	data, err := loaded.ReadPack(entry)
	if err != nil {
		return "", "", project.ReadFailureMessage(entry.Path, err)
	}
	decoded, carrierFailure := carrier.Decode(data, carrier.DefaultLimits())
	if carrierFailure != nil {
		return "", "", display.Sanitize(carrierFailure.Diagnostic.Message)
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return "", "", "The pack document is not a JSON object."
	}
	id, _ = root["id"].(string)
	version, _ = root["version"].(string)
	return id, version, ""
}
