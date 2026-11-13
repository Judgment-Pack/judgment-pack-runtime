package project

import (
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// PackRun is one declared pack's matrix run for a revalidation baseline
// (ADR-0091): the rows exactly as packs test judges them, beside the digests of
// the bytes those rows were run over.
type PackRun struct {
	// ID is the pack's key in the configuration.
	ID string
	// Ran is false when the pack or its matrix could not be read or decoded,
	// and Detail then says why. No row ran for such a pack.
	Ran    bool
	Detail string
	// PackID and PackVersion are the pack document's own identity.
	PackID      string
	PackVersion string
	// SpecVersion is the evaluator contract the pack's own declaration
	// selected, as packs test reports it in evaluatorSpecVersion.
	SpecVersion string
	// PackDigest and MatrixDigest are "sha256:" and the hex digest of the
	// exact bytes read, the form the reviewed-set lock records.
	PackDigest   string
	MatrixDigest string
	Rows         []result.EvaluationCorpusCase
}

// RunMatrices runs the matrix of every declared pack that has one, in the
// configuration's sorted id order, each pack on its own.
//
// Each pack's bytes and its matrix's bytes are read once, and those are the
// bytes that are digested and the bytes the rows run over: a digest of one
// read beside the rows of another would describe a file nobody ran. The rows
// are judged by testLoadedPack, the code packs test runs, so a row here is the
// row packs test would have reported. Each pack selects its own contract, so a
// project that mixes Core versions, which one packs test run refuses, is run
// whole.
//
// A pack or matrix that cannot be read or decoded is returned with Ran false
// and a Detail; what to make of that is the caller's. A refusal that packs
// test makes for a whole run -- an unsupported profile, a report budget
// crossed -- is returned as a Failure here too, and no partial result is
// returned with it. noMatrix lists the declared packs that have no matrix.
func (p *Project) RunMatrices(evaluator *evaluation.Engine, command string) (runs []PackRun, noMatrix []string, failure *Failure) {
	runs = []PackRun{}
	noMatrix = []string{}
	for _, id := range p.IDs {
		entry := p.Config.Packs[id]
		if entry.Matrix == "" {
			noMatrix = append(noMatrix, id)
			continue
		}
		run, failure := p.runForBaseline(evaluator, id, entry, command)
		if failure != nil {
			return nil, nil, failure
		}
		runs = append(runs, run)
	}
	return runs, noMatrix, nil
}

func (p *Project) runForBaseline(evaluator *evaluation.Engine, id string, entry Pack, command string) (PackRun, *Failure) {
	out := PackRun{ID: id}
	pack, err := p.ReadPack(entry)
	if err != nil {
		out.Detail = ReadFailureMessage(entry.Path, err)
		return out, nil
	}
	matrixRun := &matrixRun{}
	contract, failure := matrixRun.selectPackContract(pack)
	if failure != nil {
		return PackRun{}, failure
	}
	found, err := identityFrom(pack)
	if err != nil {
		out.Detail = ReadFailureMessage(entry.Path, err)
		return out, nil
	}
	cases, err := p.root.Read(entry.Matrix, MaxMatrixBytes)
	if err != nil {
		out.Detail = ReadFailureMessage(entry.Matrix, err)
		return out, nil
	}
	matrix, err := DecodeMatrix(cases)
	if err != nil {
		out.Detail = display.Sanitize(err.Error())
		return out, nil
	}
	report := result.PackTestEntry{ID: id, PackID: found.ID, PackVersion: found.Version, Path: entry.Path, MatrixPath: entry.Matrix, Status: "passed", Rows: []result.EvaluationCorpusCase{}}
	report, failure = p.testLoadedPack(evaluator, id, report, pack, matrix, command, matrixRun, contract)
	if failure != nil {
		return PackRun{}, failure
	}
	out.Ran = true
	out.PackID, out.PackVersion = found.ID, found.Version
	out.SpecVersion = matrixRun.contract.Version()
	out.PackDigest, out.MatrixDigest = audit.Digest(pack), audit.Digest(cases)
	out.Rows = report.Rows
	return out, nil
}
