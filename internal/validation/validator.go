package validation

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/uriformat"
)

const MaxDiagnostics = 100

type Options struct {
	Through             string
	ExpectedSpecVersion string
	SupportedExtensions []string
	SupportedProfiles   []string
	Limits              carrier.Limits
	// FormatOverrides is explicit companion-scoped structural validation. The
	// same bundled schema and input bytes are used; cached defaults are untouched.
	FormatOverrides []*jsonschema.Format
}

type OperationalFailure struct {
	Code     string
	Message  string
	ExitCode int
}

func (f *OperationalFailure) Error() string {
	return f.Message
}

type Engine struct {
	sets    map[string]*artifacts.Set
	schemas map[string]*jsonschema.Schema
}

func NewEngine() (*Engine, error) {
	engine := &Engine{sets: map[string]*artifacts.Set{}, schemas: map[string]*jsonschema.Schema{}}
	for _, version := range artifacts.SupportedVersions() {
		set, err := artifacts.Load(version)
		if err != nil {
			return nil, err
		}
		schemaBytes, err := set.Schema()
		if err != nil {
			return nil, err
		}
		compiled, err := CompileSchema(schemaBytes, "urn:judgmentpack:jps:"+version+":schema")
		if err != nil {
			return nil, fmt.Errorf("compile bundled schema: %w", err)
		}
		engine.sets[version] = set
		engine.schemas[version] = compiled
	}
	return engine, nil
}

func CompileSchema(schemaBytes []byte, resourceID string) (*jsonschema.Schema, error) {
	return CompileSchemas(map[string][]byte{resourceID: schemaBytes}, resourceID)
}

// CompileSchemas resolves only explicitly supplied schema resources, using the
// same URI assertions and ECMAScript expressions as bundled Core validation.
func CompileSchemas(resources map[string][]byte, resourceID string) (*jsonschema.Schema, error) {
	return CompileSchemasWithFormats(resources, resourceID)
}

// CompileSchemasWithFormats keeps the offline compiler's existing defaults and
// permits an explicit companion contract to override a named format assertion.
// Overrides never affect other compiled schemas or the bundled Core engine.
func CompileSchemasWithFormats(resources map[string][]byte, resourceID string, formats ...*jsonschema.Format) (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	compiler.RegisterFormat(&jsonschema.Format{Name: "uri", Validate: uriformat.Validate})
	for _, format := range formats {
		compiler.RegisterFormat(format)
	}
	compiler.UseLoader(offlineSchemaLoader{})
	compiler.UseRegexpEngine(ecmaRegexpCompile)
	for id, data := range resources {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode schema: %w", err)
		}
		if err := compiler.AddResource(id, document); err != nil {
			return nil, fmt.Errorf("register schema: %w", err)
		}
	}
	compiled, err := compiler.Compile(resourceID)
	if err != nil {
		return nil, err
	}
	return compiled, nil
}

type offlineSchemaLoader struct{}

func (offlineSchemaLoader) Load(string) (any, error) {
	return nil, errors.New("external schema resources are disabled")
}

type ecmaRegexp regexp2.Regexp

func (re *ecmaRegexp) MatchString(value string) bool {
	matched, err := (*regexp2.Regexp)(re).MatchString(value)
	return err == nil && matched
}

func (re *ecmaRegexp) String() string {
	return (*regexp2.Regexp)(re).String()
}

func ecmaRegexpCompile(expression string) (jsonschema.Regexp, error) {
	compiled, err := regexp2.Compile(expression, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	return (*ecmaRegexp)(compiled), nil
}

func (e *Engine) Validate(data []byte, options Options) (result.Validation, *OperationalFailure) {
	if options.Through == "" {
		options.Through = "semantic"
	}
	if options.Limits.MaxDepth == 0 {
		options.Limits = carrier.DefaultLimits()
	}
	output := result.NewValidation(options.Through)
	if options.Through != "carrier" {
		_, diagnostic, scanFailure := NegotiateProfile(data, options.Limits, options.SupportedProfiles)
		if scanFailure == nil && diagnostic != nil {
			output.SpecVersion = "0.3.0"
			output.Layers = append(output.Layers, result.Layer{Name: "carrier", Status: "passed"})
			if diagnostic.Code == "profile-unsupported" {
				output.Status = "unsupported"
				output.Diagnostics = []result.Diagnostic{*diagnostic}
				return output, nil
			}
			return structuralBootstrapFailure(output, diagnostic.InstancePath, diagnostic.Code, diagnostic.Message), nil
		}
	}

	value, carrierFailure := carrier.Decode(data, options.Limits)
	if carrierFailure != nil {
		if carrierFailure.Resource {
			return output, &OperationalFailure{
				Code:     carrierFailure.Diagnostic.Code,
				Message:  carrierFailure.Diagnostic.Message,
				ExitCode: result.ExitIO,
			}
		}
		output.Status = "invalid"
		output.Layers = []result.Layer{{Name: "carrier", Status: "failed"}}
		output.Diagnostics = []result.Diagnostic{carrierFailure.Diagnostic}
		return output, nil
	}
	output.Layers = append(output.Layers, result.Layer{Name: "carrier", Status: "passed"})
	if options.Through == "carrier" {
		output.Status = "valid"
		return output, nil
	}

	root, rootOK := value.(map[string]any)
	if !rootOK {
		return structuralBootstrapFailure(output, "", "JPS-STRUCTURE-TYPE", "The document root must be an object."), nil
	}
	rawVersion, exists := root["specVersion"]
	if !exists {
		return structuralBootstrapFailure(output, "/specVersion", "JPS-STRUCTURE-REQUIRED-MEMBER", "Required member is missing."), nil
	}
	documentVersion, versionOK := rawVersion.(string)
	if !versionOK {
		return structuralBootstrapFailure(output, "/specVersion", "JPS-STRUCTURE-TYPE", "Specification version must be a string."), nil
	}
	output.SpecVersion = documentVersion
	selectedVersion := documentVersion
	if options.ExpectedSpecVersion != "" {
		selectedVersion = options.ExpectedSpecVersion
	}
	set, setOK := e.sets[selectedVersion]
	if !setOK {
		output.Status = "unsupported"
		output.Diagnostics = []result.Diagnostic{
			result.ErrorDiagnostic("JPS-CAPABILITY-SPEC-VERSION", "capability", "/specVersion", "The exact JPS specification version is not bundled with this CLI."),
		}
		return output, nil
	}
	lock := set.Lock()
	output.Artifact = &result.Artifact{
		SpecVersion:  selectedVersion,
		BundleDigest: lock.BundleDigest.Value,
		Provenance:   lock.Source.Kind,
	}

	schema := e.schemas[selectedVersion]
	if len(options.FormatOverrides) != 0 {
		rawSchema, err := set.Schema()
		if err == nil {
			id := "urn:judgmentpack:jps:" + selectedVersion + ":schema"
			schema, err = CompileSchemasWithFormats(map[string][]byte{id: rawSchema}, id, options.FormatOverrides...)
		}
		if err != nil {
			return output, &OperationalFailure{Code: "JPS-SCHEMA-FORMAT-OVERRIDE", Message: "The bundled structural schema could not be compiled with explicit format assertions.", ExitCode: result.ExitIO}
		}
	}
	schemaErr := schema.Validate(value)
	structuralCollector := newDiagnosticCollector()
	if schemaErr != nil {
		var validationError *jsonschema.ValidationError
		if errors.As(schemaErr, &validationError) {
			collectStructural(validationError, structuralCollector, selectedVersion == artifacts.CoreVersion)
		} else {
			structuralCollector.addError("JPS-STRUCTURE-SCHEMA", "structural", "", "Structural validation failed.")
		}
	}
	extensionNameDiagnostics(root, structuralCollector)
	exceptionShapeDiagnostics(root, structuralCollector)
	if schemaErr != nil && len(structuralCollector.items) == 0 {
		structuralCollector.addError("JPS-STRUCTURE-SCHEMA", "structural", "", "Structural validation failed.")
	}
	structural := structuralCollector.sorted()
	output.DiagnosticsTruncated = structuralCollector.truncated
	if schemaErr != nil || len(structural) > 0 {
		output.Status = "invalid"
		output.Layers = append(output.Layers, result.Layer{Name: "structural", Status: "failed"})
		output.Diagnostics = structural
		return output, nil
	}
	output.Layers = append(output.Layers, result.Layer{Name: "structural", Status: "passed"})
	if options.Through == "structural" {
		output.Status = "valid"
		return output, nil
	}

	// Populate the extensions summary before the semantic gate so it reflects
	// the document's declared requirements even when the document is invalid at
	// the semantic layer. The unsupported-status decision below still runs only
	// after semantic passes, so status precedence is unchanged.
	required := requiredExtensions(root)
	supported := uniqueSorted(options.SupportedExtensions)
	unsupported := difference(required, supported)
	output.Extensions.Required = required
	output.Extensions.Supported = supported
	output.Extensions.Unsupported = unsupported

	semantic, semanticTruncated := semanticDiagnostics(root)
	if len(semantic) > 0 {
		output.Status = "invalid"
		output.Layers = append(output.Layers, result.Layer{Name: "semantic", Status: "failed"})
		output.Diagnostics = semantic
		output.DiagnosticsTruncated = semanticTruncated
		return output, nil
	}
	output.Layers = append(output.Layers, result.Layer{Name: "semantic", Status: "passed"})

	if len(unsupported) > 0 {
		output.Status = "unsupported"
		collector := newDiagnosticCollector()
		unsupportedSet := stringSet(unsupported)
		for index, extension := range required {
			if unsupportedSet[extension] {
				if !collector.add(result.ErrorDiagnostic(
					"JPS-CAPABILITY-REQUIRED-EXTENSION",
					"capability",
					fmt.Sprintf("/metadata/requiredExtensions/%d", index),
					"A required extension capability is not supported.",
				)) {
					break
				}
			}
		}
		output.Diagnostics = collector.sorted()
		output.DiagnosticsTruncated = collector.truncated
		return output, nil
	}
	output.Status = "valid"
	// Core 0.3.0 introduces this recommended, non-normative check. Earlier
	// contracts and admission/evaluation statuses retain their existing behavior.
	if selectedVersion == artifacts.CoreVersion {
		output.Diagnostics, output.DiagnosticsTruncated = ruleOverlapDiagnosticsWithLimits(root, options.Limits)
	}
	return output, nil
}

func structuralBootstrapFailure(output result.Validation, location, code, message string) result.Validation {
	output.Status = "invalid"
	output.Layers = append(output.Layers, result.Layer{Name: "structural", Status: "failed"})
	output.Diagnostics = []result.Diagnostic{result.ErrorDiagnostic(code, "structural", location, message)}
	return output
}

func collectStructural(validationError *jsonschema.ValidationError, diagnostics *diagnosticCollector, core030 bool) {
	if diagnostics.full() {
		diagnostics.truncated = true
		return
	}
	location := append([]string(nil), validationError.InstanceLocation...)
	add := func(code string, parts []string, message string) bool {
		return diagnostics.addError(code, "structural", carrier.Pointer(parts), message)
	}
	// These new Core 0.3 route constraints have corpus-pinned diagnostics.
	// Keep legacy schemas and condition-shape diagnostics unchanged.
	if core030 {
		switch validationError.ErrorKind.(type) {
		case *kind.OneOf:
			if len(location) == 1 && location[0] == "escalation" {
				add("JPS-STRUCTURE-ONEOF", location, "Escalation must declare exactly one of target or routes.")
				return
			}
		case *kind.MaxItems:
			if len(location) == 2 && location[0] == "escalation" && location[1] == "routes" {
				add("JPS-STRUCTURE-MAXITEMS", location, "Escalation routes exceed the selected profile limit.")
				return
			}
		case *kind.Not:
			if len(location) == 4 && location[0] == "escalation" && location[1] == "routes" && location[3] == "triggers" {
				add("JPS-STRUCTURE-NOT", location, "Route triggers must be a subset of escalation triggers.")
				return
			}
		}
	}
	handled := false

	switch typed := validationError.ErrorKind.(type) {
	case *kind.Required:
		handled = true
		for _, missing := range typed.Missing {
			// A missing effect-conditional operand is reported precisely, naming
			// the effect, by exceptionShapeDiagnostics; suppress it here.
			if contains(location, "exceptions") && (missing == "outcome" || missing == "targetRule") {
				continue
			}
			if !add("JPS-STRUCTURE-REQUIRED-MEMBER", append(location, missing), "Required member is missing.") {
				return
			}
		}
	case *kind.AdditionalProperties:
		handled = true
		for _, property := range typed.Properties {
			if !add("JPS-STRUCTURE-UNKNOWN-MEMBER", append(location, property), "Member is not allowed in this Core object.") {
				return
			}
		}
	case *kind.PropertyNames:
		handled = true
		// Core-aware extension-name checks below provide the stable absolute path.
	case *kind.Format:
		handled = true
		add("JPS-STRUCTURE-FORMAT-"+strings.ToUpper(typed.Want), location, "Value does not satisfy the required "+typed.Want+" format.")
	case *kind.Pattern:
		handled = true
		code := patternCode(typed.Want)
		if code != "JPS-STRUCTURE-EXTENSION-NAME" {
			add(code, location, patternMessage(code, typed))
		}
	case *kind.MinItems:
		handled = true
		code := "JPS-STRUCTURE-COLLECTION-ARITY"
		if last(location) == "conditions" {
			code = "JPS-STRUCTURE-CONDITION-ARITY"
		} else if last(location) == "triggers" {
			code = "JPS-STRUCTURE-ESCALATION-TRIGGERS"
		}
		add(code, location, arityMessage(code, typed))
	case *kind.Type:
		handled = true
		switch {
		case comparisonOperandValue(location) && contains(typed.Want, "array"):
			add("JPS-STRUCTURE-IN-OPERAND", location, structuralMessage("JPS-STRUCTURE-IN-OPERAND"))
		case comparisonOperandValue(location) && contains(typed.Want, "string"):
			add("JPS-STRUCTURE-DECIMAL-OPERAND", location, structuralMessage("JPS-STRUCTURE-DECIMAL-OPERAND"))
		default:
			add("JPS-STRUCTURE-TYPE", location, typeMessage(typed))
		}
	case *kind.Const:
		if last(location) == "specVersion" {
			handled = true
			add("JPS-STRUCTURE-SPEC-VERSION", location, "Specification version does not match the selected JPS release.")
		}
	case *kind.Enum:
		handled = true
		add("JPS-STRUCTURE-ENUM", location, enumMessage(typed))
	case *kind.MinLength:
		handled = true
		add("JPS-STRUCTURE-MIN-LENGTH", location, "String is shorter than the required minimum length.")
	case *kind.UniqueItems:
		handled = true
		add("JPS-STRUCTURE-UNIQUE-ITEMS", location, "Array items must be unique.")
	case *kind.OneOf:
		handled = true
		relevantCauses := make([]*jsonschema.ValidationError, 0, len(validationError.Causes))
		for _, cause := range validationError.Causes {
			if branchRejectedByDiscriminator(cause) {
				continue
			}
			relevantCauses = append(relevantCauses, cause)
		}
		if len(relevantCauses) == 0 {
			add("JPS-STRUCTURE-CONDITION-SHAPE", location, "Condition does not match one declared shape; its op must be one of: literal, all, any, not, fact, evidence-present.")
			return
		}
		for _, cause := range relevantCauses {
			collectStructural(cause, diagnostics, core030)
		}
		return
	case *kind.Not, *kind.AllOf:
		// The exception effect/operand constraints (an allOf of if/then/not) fail
		// opaquely at the exception object. exceptionShapeDiagnostics reports them
		// precisely at the offending member, so suppress the schema-level failure
		// here. Guard on the exception object itself so a condition allOf nested
		// inside an exception's "when" still reports normally.
		if n := len(location); n >= 2 && location[n-2] == "exceptions" {
			handled = true
		}
	case *kind.Schema, *kind.Reference, *kind.Group:
		handled = true
	}
	if !handled && len(validationError.Causes) == 0 {
		add("JPS-STRUCTURE-SCHEMA", location, "Value does not satisfy a structural constraint.")
	}
	for _, cause := range validationError.Causes {
		collectStructural(cause, diagnostics, core030)
	}
}

func patternCode(pattern string) string {
	switch {
	case pattern == "^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$":
		return "JPS-STRUCTURE-LOCAL-ID"
	case strings.HasPrefix(pattern, "^(0|[1-9][0-9]*)"):
		return "JPS-STRUCTURE-PACK-VERSION"
	case pattern == "^(?:/(?:[^~/]|~0|~1)*)*$":
		return "JPS-STRUCTURE-FACT-PATH"
	case pattern == "^-?(?:0|[1-9][0-9]*)(?:\\.[0-9]+)?$":
		return "JPS-STRUCTURE-DECIMAL-OPERAND"
	case strings.Contains(pattern, "org\\.judgmentpack"):
		return "JPS-STRUCTURE-EXTENSION-NAME"
	default:
		return "JPS-STRUCTURE-PATTERN"
	}
}

func structuralMessage(code string) string {
	switch code {
	case "JPS-STRUCTURE-LOCAL-ID":
		return "Local identifier has an invalid shape."
	case "JPS-STRUCTURE-PACK-VERSION":
		return "Pack version has an invalid shape."
	case "JPS-STRUCTURE-FACT-PATH":
		return "Fact path is not a valid JSON Pointer."
	case "JPS-STRUCTURE-DECIMAL-OPERAND":
		return "Ordered comparison operand must be a decimal string, for example \"5000\"."
	case "JPS-STRUCTURE-EXTENSION-NAME":
		return "Extension name must use an allowed reverse-domain namespace."
	case "JPS-STRUCTURE-CONDITION-ARITY":
		return "Condition requires at least one child condition."
	case "JPS-STRUCTURE-ESCALATION-TRIGGERS":
		return "Escalation requires at least one trigger."
	case "JPS-STRUCTURE-COLLECTION-ARITY":
		return "Collection does not contain the required number of items."
	case "JPS-STRUCTURE-IN-OPERAND":
		return "The in operator requires an array operand."
	case "JPS-STRUCTURE-TYPE":
		return "Value has the wrong JSON type."
	default:
		return "Value does not satisfy a structural constraint."
	}
}

// enumMessage lists the allowed values so an author does not have to open the
// schema to learn them.
func enumMessage(typed *kind.Enum) string {
	allowed := make([]string, 0, len(typed.Want))
	for _, value := range typed.Want {
		allowed = append(allowed, stringifyValue(value))
	}
	if len(allowed) == 0 {
		return "Value is not one of the allowed values."
	}
	return "Value is not one of the allowed values: " + joinCapped(allowed, 12) + "."
}

func stringifyValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

// patternMessage names the offending value and gives a concrete example rather
// than dumping the grammar regex, for each pattern-constrained member.
func patternMessage(code string, typed *kind.Pattern) string {
	switch code {
	case "JPS-STRUCTURE-LOCAL-ID":
		return fmt.Sprintf("Local identifier %q has an invalid shape; use lowercase words joined by single hyphens, for example %q.", typed.Got, "needs-review")
	case "JPS-STRUCTURE-PACK-VERSION":
		return fmt.Sprintf("Pack version %q has an invalid shape; use MAJOR.MINOR.PATCH, for example %q.", typed.Got, "1.0.0")
	case "JPS-STRUCTURE-FACT-PATH":
		return fmt.Sprintf("Fact path %q is not a valid JSON Pointer; it must be empty or begin with %q, and any %q must be escaped as %q or %q, for example %q.", typed.Got, "/", "~", "~0", "~1", "/amount")
	case "JPS-STRUCTURE-DECIMAL-OPERAND":
		return fmt.Sprintf("Ordered comparison operand %q must be a decimal string, for example %q.", typed.Got, "5000")
	default:
		return fmt.Sprintf("Value %q does not satisfy the required pattern.", typed.Got)
	}
}

// arityMessage states the required minimum and the actual count for a
// too-short collection.
func arityMessage(code string, typed *kind.MinItems) string {
	switch code {
	case "JPS-STRUCTURE-CONDITION-ARITY":
		return fmt.Sprintf("Condition needs at least %d child condition%s but has %d.", typed.Want, plural(typed.Want), typed.Got)
	case "JPS-STRUCTURE-ESCALATION-TRIGGERS":
		return fmt.Sprintf("Escalation needs at least %d trigger%s but has %d.", typed.Want, plural(typed.Want), typed.Got)
	default:
		return fmt.Sprintf("Collection needs at least %d item%s but has %d.", typed.Want, plural(typed.Want), typed.Got)
	}
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func last(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// comparisonOperandValue reports whether an instance location denotes the
// "value" operand of a fact comparison condition, as opposed to another member
// that happens to be named "value" (such as a source locator value). Only a
// comparison operand should carry an operator-specific diagnostic.
func comparisonOperandValue(location []string) bool {
	n := len(location)
	if n < 2 || location[n-1] != "value" {
		return false
	}
	switch location[n-2] {
	case "when", "applicability", "condition":
		return true
	}
	return n >= 3 && location[n-3] == "conditions"
}

// typeMessage names the expected and actual JSON types so an author does not
// have to consult the schema to learn what a member should have been.
func typeMessage(typed *kind.Type) string {
	want := strings.Join(typed.Want, " or ")
	switch {
	case want == "":
		return "Value has the wrong JSON type."
	case typed.Got == "":
		return fmt.Sprintf("Value must be of type %s.", want)
	default:
		return fmt.Sprintf("Value must be of type %s, but is %s.", want, typed.Got)
	}
}

// branchRejectedByDiscriminator reports whether a oneOf branch failed solely
// because its "op" discriminator did not match, which makes its remaining
// errors noise from an alternative the author never intended. A branch whose op
// matched but that failed deeper is kept, even when the schema library groups
// several of its sub-failures together -- otherwise a composite condition with
// more than one invalid child collapses to a single generic shape error and
// loses the specific, well-located diagnostics for each child.
func branchRejectedByDiscriminator(cause *jsonschema.ValidationError) bool {
	// A losing branch that failed on its op discriminator directly, as a single
	// bare failure rather than a group -- otherwise that lone op-const/enum
	// failure leaks out as a diagnostic anchored at an op that is actually valid.
	if discriminatorMismatch(cause) {
		return true
	}
	// A losing branch whose failures were grouped, one of which is the op
	// discriminator.
	if _, isGroup := cause.ErrorKind.(*kind.Group); isGroup {
		for _, sub := range cause.Causes {
			if discriminatorMismatch(sub) {
				return true
			}
		}
	}
	return false
}

// discriminatorMismatch reports whether a sub-error is a failure of the "op"
// discriminator itself: a wrong constant or enum value at op, or a missing op
// member. Only the branches whose op did not match produce these.
func discriminatorMismatch(ve *jsonschema.ValidationError) bool {
	switch ve.ErrorKind.(type) {
	case *kind.Const, *kind.Enum:
		return last(ve.InstanceLocation) == "op"
	}
	if required, ok := ve.ErrorKind.(*kind.Required); ok {
		return contains(required.Missing, "op")
	}
	return false
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	output := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			output = append(output, value)
		}
	}
	slices.Sort(output)
	return output
}

func difference(required, supported []string) []string {
	supportedSet := stringSet(supported)
	output := []string{}
	for _, value := range required {
		if !supportedSet[value] {
			output = append(output, value)
		}
	}
	return output
}

func stringSet(values []string) map[string]bool {
	output := make(map[string]bool, len(values))
	for _, value := range values {
		output[value] = true
	}
	return output
}
