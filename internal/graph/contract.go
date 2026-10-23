package graph

import (
	"fmt"

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
	return evaluation.SelectContract(data)
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
