package federation

import "github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"

// Severity of a diagnostic, as format 1.0.0 states it.
const (
	SeverityError   = registry.SeverityError
	SeverityWarning = registry.SeverityWarning
)

// codeInfo is one code's severity and fixed message.
type codeInfo struct {
	severity string
	message  string
}

// Codes is the closed list of the codes of registry-1.1.0 §12, each an error
// and active, with its fixed message: the text's meaning, as codes.json gives
// it. None is raised by a reader of format 1.0.0, and the codes of 1.0.0
// (registry.Codes) keep their meanings beside these.
var Codes = map[string]codeInfo{
	// §12.1: assembly, the assembled index and bounds.
	"FED-MANIFEST":                  {SeverityError, "The manifest is not of its form."},
	"FED-COMMIT":                    {SeverityError, "A commit is not 40 or 64 lowercase hexadecimal digits, or two sources of one repository identity are given different commits."},
	"FED-REPOSITORY":                {SeverityError, "A repository identity is not an absolute URI of its bound, or holds userinfo, a query or a fragment."},
	"FED-SOURCE-MISSING":            {SeverityError, "The bytes of a path the manifest names were not given."},
	"FED-ENVIRONMENTS":              {SeverityError, "A source's registry's environments are not the environments it serves."},
	"FED-ENVIRONMENT-GAP":           {SeverityError, "In an environment its entry applies in, a reference's target is held only by a source that does not serve it."},
	"FED-KNOWN-LIMIT":               {SeverityError, "The list of known assembled indexes holds more than 256 indexes or 256 locks."},
	"FED-SOURCE-ASSEMBLED":          {SeverityError, "A source's registry is one that a known assembled index or lock names."},
	"FED-REGISTRY-DUPLICATE":        {SeverityError, "Two registries of the assembly share an id."},
	"FED-KIND-NOT-STANDARD":         {SeverityError, "A governed kind is pinned to a declaration the standard does not supply."},
	"FED-KIND-CONFLICT":             {SeverityError, "Two sources pin one kind name to different declarations."},
	"FED-SCOPE-WIDENS":              {SeverityError, "A repository's secret scopes list a project the standard's do not, for that environment and store."},
	"FED-BOUND-FORM":                {SeverityError, "A bounds document is not of its form, or a repository's states maxAssemblyEntries."},
	"FED-BOUND-ENVIRONMENT":         {SeverityError, "A per-environment bound names an environment the manifest does not, or the repository does not serve."},
	"FED-BOUND-KIND":                {SeverityError, "A bounded kind is neither governed nor pinned by the assembled registry, or a standard bound names a kind it does not govern."},
	"FED-BOUND-POINTER-UNREACHABLE": {SeverityError, "A field's pointer reaches no position of the kind's schema."},
	"FED-BOUND-NOT-COMPARABLE":      {SeverityError, "A bound holds a keyword of §6.3."},
	"FED-BOUND-OVERLAP":             {SeverityError, "A repository field's pointer overlaps a standard field's without being equal to it."},
	"FED-BOUND-WIDENS":              {SeverityError, "A bound is looser than the one it narrows on a row, or names a value that one refuses."},
	"FED-BOUND-ENTRIES":             {SeverityError, "A source holds more entries of a kind than a maxEntries allows, or the assembly more than maxAssemblyEntries."},
	"FED-VALUE-BOUND":               {SeverityError, "A resolved value fails a bound."},
	"FED-INDEX-MISMATCH":            {SeverityError, "An index check finds a difference."},

	// §12.2: locks, built. The lock writer (§§7, 8) and the lock checker (§9)
	// raise these. A writer given an assembly verifies each source signature
	// as §11 does before it names it (§8), and refuses one that does not
	// verify with that signature's code, so the codes of reading and checking a
	// signature document are raised here too.
	"LOCK-EXPECTED":            {SeverityError, "An expected input is missing (the registry id, the index requirement, the environment for coverage, a repository source's key, or a floor at first contact); a repository key map is given without an index required, or the reverse; one key has two roles in the set, or the standard item's repository is one the map names; or the id is not an absolute URI."},
	"LOCK-FORM":                {SeverityError, "The lock is not of its form, or a writer's next serial would be out of range."},
	"LOCK-NOT-CANONICAL":       {SeverityError, "The lock's bytes are not its JCS bytes."},
	"LOCK-UNEXPECTED-REGISTRY": {SeverityError, "The lock's registry.id, or a writer's previous lock's, is not the expected registry id."},
	"LOCK-INDEX-REQUIRED":      {SeverityError, "An index is required and the lock names none."},
	"LOCK-REGISTRY-INVALID":    {SeverityError, "The registry, or the assembly, has an error."},
	"LOCK-REGISTRY-MISMATCH":   {SeverityError, "The registry is not the one the lock names."},
	"LOCK-KINDS-MISMATCH":      {SeverityError, "The lock's kinds are not the registry's pins."},
	"LOCK-RESOLVED-MISMATCH":   {SeverityError, "An environment's resolved digest differs, or the environments do."},
	"LOCK-INDEX-MISSING":       {SeverityError, "The lock names an index and none was given, to a check or a verification."},
	"LOCK-INDEX-MISMATCH":      {SeverityError, "The index is not the one the lock names, names another registry, or does not have exactly one standard item."},
	"LOCK-INDEX-UNLOCKED":      {SeverityError, "An index was given for a lock that names none."},
	"LOCK-SOURCES-MISMATCH":    {SeverityError, "The lock's sources are not the index's, or a statement digest differs."},
	"LOCK-EMITTER-MISSING":     {SeverityError, "The lock names emitters and none were given, or a manifest or file it lists was not given."},
	"LOCK-EMITTER-MISMATCH":    {SeverityError, "An emitter manifest or owned file given differs from the lock's item, an owned file is absent from the lock, or a host check did not exit 0."},
	"LOCK-NOT-COVERED":         {SeverityError, "A resolved registry is not of the expected environment, or not the one the lock names for it; or a file is not one the lock lists."},
	"LOCK-SIG-EXPECTED-KEY":    {SeverityError, "An expected key is missing, or one a verifier may not trust."},
	"LOCK-SIG-MISSING":         {SeverityError, "The lock signature, or an item's source signature, is not given."},
	"LOCK-SIG-EXTRA":           {SeverityError, "A second lock signature is given, a source signature that no item expects or that is given twice, or more signature documents than the lock's sources count plus one."},
	"LOCK-SIG-DOCUMENT":        {SeverityError, "A signature document is over its bound, not one JSON object, repeats a member or holds a backslash."},
	"LOCK-SIG-VERSION":         {SeverityError, "LockSignatureVersion or sourceSignatureVersion is not \"1.1.0\"."},
	"LOCK-SIG-MEMBER":          {SeverityError, "A member is unknown or missing."},
	"LOCK-SIG-ALGORITHM":       {SeverityError, "Algorithm is not ed25519."},
	"LOCK-SIG-PUBLIC-KEY":      {SeverityError, "PublicKey is not of its form, or not a key a verifier may trust."},
	"LOCK-SIG-FORM":            {SeverityError, "A digest member or signature is not of its form."},
	"LOCK-SIG-UNEXPECTED-KEY":  {SeverityError, "A document's key is not the key the expected set names for it."},
	"LOCK-SIG-DIGEST-MISMATCH": {SeverityError, "The lock's bytes, or a source's statement, are not what the document names."},
	"LOCK-SIG-INVALID":         {SeverityError, "A signature does not hold under its expected key."},

	// §12.2: locks and signatures, pending. The lock signature verifier class
	// (§15) is not built yet, so no reader of this module raises these codes:
	// only its serial record (§11, Rollback) does. The mutation table holds a
	// code under this heading to no row, and its check fails once a non-test
	// file names one outside this table, or once no class is left to build.
	"LOCK-SERIAL-ROLLBACK": {SeverityError, "The lock's serial is below the verifier's record, or below its floor."},
	"LOCK-SERIAL-CONFLICT": {SeverityError, "The lock's serial is the record's and its digest is not."},
	"LOCK-SERIAL-STATE":    {SeverityError, "The verifier's record exists and cannot be read or is not of its form, or cannot be written."},
}
