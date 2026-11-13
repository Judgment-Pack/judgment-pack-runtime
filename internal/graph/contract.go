package graph

import (
	"fmt"
	"maps"
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// SelectContract checks every participating pack before execution. Graphs apply
// one exact evaluator contract; mixed Core versions cannot share an envelope.
// Packs are re-read and checked at evaluation so this scan retains no pack
// buffers and never supplies provenance for a different evaluation read.
func SelectContract(loaded *project.Project, doc Document) (result.EvaluatorContract, *evaluation.Failure) {
	order, _ := doc.Order()
	contract := result.LegacyEvaluatorContract()
	for index, node := range order {
		pack := doc.Nodes[node].Pack
		entry, _ := loaded.Entry(pack)
		data, oversized, failure := readNodePack(loaded, node, pack, entry)
		if failure != nil {
			return result.EvaluatorContract{}, failure
		}
		selected, failure := selectNodeContract(data, oversized)
		if failure != nil {
			return result.EvaluatorContract{}, nodeFailure(node, pack, failure)
		}
		if index > 0 && selected != contract {
			return result.EvaluatorContract{}, mixedContractFailure()
		}
		contract = selected
	}
	return contract, nil
}

func selectNodeContract(data []byte, oversized []string) (result.EvaluatorContract, *evaluation.Failure) {
	if len(oversized) > 0 {
		return result.EvaluatorContract{}, &evaluation.Failure{Class: result.ClassPackNotConformant, Phase: result.PhasePreflight, Code: "JPS-RESOURCE-INPUT-BYTE-LIMIT", Message: fmt.Sprintf("The pack input exceeds the hard %d-byte limit.", carrier.HardMaxBytes), ExitCode: result.ExitIO}
	}
	contract, failure := evaluation.SelectContract(data)
	if failure == nil && contract.Core050Rules() {
		// Graphs do not read Core 0.5.0: the graph prototype (Draft RFC 0002)
		// is written against Core 0.3.0, and a graph report is not designed
		// over annotations or value sets.
		return result.EvaluatorContract{}, core050Refusal("Graph evaluation", contract.Version())
	}
	return contract, failure
}

// Core050NodeRefusal refuses a graph one of whose node packs declares Core
// 0.5.0. No graph surface reads one: the graph prototype (Draft RFC 0002) is
// written against Core 0.3.0, and a graph report is not designed over
// annotations or value sets. Every surface that reads node packs (validate,
// explain, test and evaluate) asks it first, so a graph is refused whole, at
// the top, rather than planned, or tested row by row, under a contract it
// does not have. Every node's pack is read, in node-name order, whatever
// another node's read gives; a pack that is not declared, cannot be read, is
// oversized or selects no contract is left to the surface's own report of it,
// so a graph without a 0.5.0 node is never refused here. surface names the
// refusing surface in the message.
func Core050NodeRefusal(loaded *project.Project, doc Document, surface string) *evaluation.Failure {
	for _, node := range slices.Sorted(maps.Keys(doc.Nodes)) {
		pack := doc.Nodes[node].Pack
		entry, declared := loaded.Entry(pack)
		if !declared {
			continue
		}
		data, oversized, failure := readNodePack(loaded, node, pack, entry)
		if failure != nil || len(oversized) > 0 {
			continue
		}
		if contract, failure := evaluation.SelectContract(data); failure == nil && contract.Core050Rules() {
			return nodeFailure(node, pack, core050Refusal(surface, contract.Version()))
		}
	}
	return nil
}

// graphReadable lists the Core versions a graph reads: the evaluator
// contracts other than Core 0.5.0's.
var graphReadable = []string{result.EvaluatorSpecVersion, "0.3.0"}

// core050Meanwhile is what a graph's Core 0.5.0 refusal points to: the
// surfaces that read such a pack on its own, and where this runtime states
// what graphs wait for (ADR-0089).
const core050Meanwhile = "Meanwhile, experimental evaluate and packs test read a Core 0.5.0 pack on its own, as its evaluation form; CONFORMANCE.md states what graphs wait for."

// core050Refusal is the refusal of a Core 0.5.0 node pack: the shared surface
// refusal, its code and exit unchanged, and the message followed by
// core050Meanwhile.
func core050Refusal(surface, version string) *evaluation.Failure {
	failure := evaluation.SurfaceSpecVersionUnsupported(surface, version, graphReadable)
	failure.Message += " " + core050Meanwhile
	return failure
}

func mixedContractFailure() *evaluation.Failure {
	return &evaluation.Failure{Code: "JPS-GRAPH-MIXED-CONTRACT", Message: "A graph requires one exact evaluator contract across all participating packs.", ExitCode: result.ExitUnsupported}
}

func nodeFailure(node, pack string, failure *evaluation.Failure) *evaluation.Failure {
	if failure.Code == "profile-unsupported" {
		return failure
	}
	wrapped := *failure
	wrapped.Message = fmt.Sprintf("Node %q (pack %q): %s", display.Sanitize(node), display.Sanitize(pack), failure.Message)
	return &wrapped
}
