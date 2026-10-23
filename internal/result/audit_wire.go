package result

import "encoding/json"

// AuditFile describes the exact retained prefix of one configured audit file.
// Missing and present-but-empty files are distinct. Digest is absent only for
// a missing file; it hashes every retained byte, including line terminators.
type AuditFile struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	Bytes   int64  `json:"bytes"`
	Digest  string `json:"digest,omitempty"`
}

// AuditPageRecord describes a line when its final byte has been transported,
// even when earlier raw pages carried its beginning. Bytes and Digest exclude
// the newline, as the audit chain and definition history do. Status is parsed,
// malformed, oversized or incomplete. Only parsed carries Members; bounded
// lines also carry their exact bytes in RawLineBase64.
type AuditPageRecord struct {
	Offset        int64           `json:"offset"`
	Bytes         int64           `json:"bytes"`
	Digest        string          `json:"digest"`
	Status        string          `json:"status"`
	RawLineBase64 string          `json:"rawLineBase64,omitempty"`
	Members       json.RawMessage `json:"members,omitempty"`
}

// AuditPage transports a bounded exact byte range. An export visits all three
// files in Files order. Only Complete with no NextCursor finishes an export.
// PendingLine says a later page will finish a line begun in this page.
type AuditPage struct {
	OutputVersion         string            `json:"outputVersion"`
	Tool                  Tool              `json:"tool"`
	Command               string            `json:"command"`
	AuditWireVersion      string            `json:"auditWireVersion"`
	Files                 []AuditFile       `json:"files"`
	SnapshotBetweenWrites bool              `json:"snapshotBetweenWrites"`
	File                  string            `json:"file"`
	Offset                int64             `json:"offset"`
	DataBase64            string            `json:"dataBase64"`
	Records               []AuditPageRecord `json:"records"`
	PendingLine           bool              `json:"pendingLine"`
	FileComplete          bool              `json:"fileComplete"`
	Complete              bool              `json:"complete"`
	NextCursor            string            `json:"nextCursor,omitempty"`
}
