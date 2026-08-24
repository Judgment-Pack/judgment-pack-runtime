package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// This file serves declared graph documents and their inventory (ADR-0029):
// the graph siblings of the project's Inventory and Document, living here
// rather than in internal/project because the import points this way — the
// graph surface consumes the project convention, never the reverse — and
// because the byte limit these reads run under is this package's own
// (MaxGraphBytes), by ADR-0017's deliberate decision.

// EmptyInventory is the graph inventory of a project with no configuration at
// all: an answer with its own explanation, never an error, exactly as the pack
// inventory's is.
func EmptyInventory(configPath, command string) result.GraphInventory {
	return result.GraphInventory{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        "none",
		Experimental:  true,
		Kind:          result.ProjectKind,
		ConfigPath:    configPath,
		Note: fmt.Sprintf("No project configuration was found at %s, so this project configures no graphs. The convention is optional: a %s at the project root, or the %s environment variable, makes one.",
			display.Sanitize(configPath), project.DefaultConfigName, project.ConfigEnv),
		Graphs: []result.GraphSummary{},
	}
}

// ProjectInventory is the resolved, token-cheap description of every
// configured graph: the configured id, the document's own id and version, the
// declared result node, the paths, whether rows are declared, and the
// configuration's description. It is what a client reads to learn what a
// project composes without fetching a document — the inventory the
// configuration schema's own description text has anticipated since ADR-0017.
//
// A graph whose document cannot be read or decoded is listed with its Detail
// saying why and its identity members empty. Listing is not validating: the
// document is identity-read leniently, exactly as a pack inventory row is, and
// experimental graph validate is where a broken graph is an error.
func ProjectInventory(loaded *project.Project, command string) result.GraphInventory {
	inventory := result.GraphInventory{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        "resolved",
		Experimental:  true,
		Kind:          result.ProjectKind,
		ConfigPath:    loaded.ConfigPath,
		ConfigVersion: loaded.Config.ConfigVersion,
		Graphs:        []result.GraphSummary{},
	}
	for _, id := range loaded.GraphIDs {
		entry, _ := loaded.GraphEntry(id)
		row := result.GraphSummary{
			ID:          id,
			Path:        entry.Path,
			RowsPath:    entry.Rows,
			Rows:        entry.Rows != "",
			Description: entry.Description,
		}
		found, _, err := readGraphIdentity(loaded, entry)
		if err != nil {
			row.Detail = err.Error()
		} else {
			row.GraphID = found.id
			row.GraphVersion = found.version
			row.FormatVersion = found.formatVersion
			row.ResultNode = found.resultNode
			row.Nodes = found.nodes
			row.Edges = found.edges
		}
		inventory.Graphs = append(inventory.Graphs, row)
	}
	return inventory
}

// ProjectDocument serves one configured graph document by its configured id:
// the exact bytes beside their metadata, read through the project's own
// directory handle under this package's byte limit. Serving is not validating
// — a mid-edit graph is still served, with Status "undecodable" and Detail
// saying why, so the one moment a client most needs to see the document is
// not the moment the tool goes silent.
func ProjectDocument(loaded *project.Project, id, command string) (result.GraphDocument, []byte, *project.Failure) {
	entry, ok := loaded.GraphEntry(id)
	if !ok {
		return result.GraphDocument{}, nil, loaded.UnknownGraphFailure(id)
	}
	found, data, err := readGraphIdentity(loaded, entry)
	if err != nil && data == nil {
		return result.GraphDocument{}, nil, &project.Failure{
			Code:     "JPS-PROJECT-GRAPH-READ",
			Message:  err.Error(),
			ExitCode: result.ExitIO,
		}
	}
	status, detail := "valid", ""
	if err != nil {
		status, detail = "undecodable", err.Error()
	}
	sum := sha256.Sum256(data)
	return result.GraphDocument{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        status,
		Experimental:  true,
		Kind:          result.ProjectKind,
		ConfigPath:    loaded.ConfigPath,
		ID:            id,
		GraphID:       found.id,
		GraphVersion:  found.version,
		FormatVersion: found.formatVersion,
		ResultNode:    found.resultNode,
		Path:          entry.Path,
		RowsPath:      entry.Rows,
		Description:   entry.Description,
		Bytes:         len(data),
		SHA256:        hex.EncodeToString(sum[:]),
		Detail:        detail,
	}, data, nil
}

// graphIdentity is what a lenient identity read pulls off a graph document's
// bytes: the members an inventory or a served document echoes, none of them a
// verdict. Counts are of the declared collections as carried, whatever their
// element shapes.
type graphIdentity struct {
	id            string
	version       string
	formatVersion string
	resultNode    string
	nodes         int
	edges         int
}

// readGraphIdentity reads one configured graph and pulls its identity off the
// bytes it just read — never off a second read, so a report can not name one
// revision of a file and describe another. A read failure returns nil bytes; a
// decode failure returns the bytes it could not decode, so a caller serving
// the document still has them.
func readGraphIdentity(loaded *project.Project, entry project.Graph) (graphIdentity, []byte, error) {
	data, err := loaded.ReadGraph(entry, MaxGraphBytes)
	if err != nil {
		if errors.Is(err, fssecure.ErrTooLarge) {
			return graphIdentity{}, nil, fmt.Errorf("The graph document %s exceeds the %d-byte limit.", display.Sanitize(entry.Path), MaxGraphBytes)
		}
		return graphIdentity{}, nil, errors.New(project.ReadFailureMessage(entry.Path, err))
	}
	found, err := graphIdentityFrom(data)
	return found, data, err
}

// graphIdentityFrom pulls the identity out of bytes already read, the graph
// sibling of the pack inventory's lenient read: a bare carrier decode and
// member reads, deliberately not the closed-schema Load, because serving and
// listing must survive the mid-edit document that validation exists to refuse.
func graphIdentityFrom(data []byte) (graphIdentity, error) {
	document, carrierFailure := carrier.Decode(data, carrier.DefaultLimits())
	if carrierFailure != nil {
		return graphIdentity{}, fmt.Errorf("graph document is not acceptable JSON: %s", carrierFailure.Diagnostic.Message)
	}
	root, ok := document.(map[string]any)
	if !ok {
		return graphIdentity{}, fmt.Errorf("graph document root must be a JSON object")
	}
	found := graphIdentity{
		id:            stringAt(root, "id"),
		version:       stringAt(root, "version"),
		formatVersion: stringAt(root, "formatVersion"),
		resultNode:    stringAt(root, "result"),
	}
	if nodes, ok := root["nodes"].(map[string]any); ok {
		found.nodes = len(nodes)
	}
	if edges, ok := root["edges"].([]any); ok {
		found.edges = len(edges)
	}
	return found, nil
}

func stringAt(root map[string]any, member string) string {
	value, _ := root[member].(string)
	return value
}
