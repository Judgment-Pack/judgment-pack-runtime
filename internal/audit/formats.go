package audit

import "reflect"

// LineFormat is one persisted line type of a project's audit trail: the file
// it is written to, its name, the format version the writer stamps on that
// file's lines, and the Go type it is encoded from. The catalog in
// internal/auditformat fingerprints each one (#327), so a change to a member
// of a persisted line cannot pass without a committed format decision.
type LineFormat struct {
	File    string
	Line    string
	Version string
	Type    reflect.Type
}

// LineFormats lists every line type this runtime persists, in file order. A
// new persisted line type is added here, or the catalog cannot fingerprint it.
func LineFormats() []LineFormat {
	return []LineFormat{
		// One shape carries both decision kinds, evaluation and graph-composite.
		{FileName, "record", RecordVersion, reflect.TypeOf(Record{})},
		{FileName, KindDiscontinuity, RecordVersion, reflect.TypeOf(discontinuityRecord{})},
		{SidecarName, KindRecordSignature, SidecarVersion, reflect.TypeOf(recordSignatureLine{})},
		{SidecarName, KindKeyRotation, SidecarVersion, reflect.TypeOf(keyRotationLine{})},
		{StampsName, "stamp", StampVersion, reflect.TypeOf(stampLine{})},
	}
}
