package validation

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const MaxDiagnostics = 100

type Options struct {
	Through             string
	ExpectedSpecVersion string
	SupportedExtensions []string
	Limits              carrier.Limits
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
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, fmt.Errorf("decode schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	compiler.RegisterFormat(&jsonschema.Format{Name: "uri", Validate: validateRFC3986URI})
	compiler.UseLoader(offlineSchemaLoader{})
	compiler.UseRegexpEngine(ecmaRegexpCompile)
	if err := compiler.AddResource(resourceID, document); err != nil {
		return nil, fmt.Errorf("register schema: %w", err)
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

var uriSchemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*$`)

func validateRFC3986URI(value any) error {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	if text == "" {
		return errors.New("URI is empty")
	}
	for index := 0; index < len(text); index++ {
		character := text[index]
		if character == '%' {
			if index+2 >= len(text) || !isHex(text[index+1]) || !isHex(text[index+2]) {
				return errors.New("URI contains an invalid percent escape")
			}
			index += 2
			continue
		}
		if !isRFC3986Character(character) {
			return errors.New("URI contains a character outside RFC 3986")
		}
	}
	if strings.Count(text, "#") > 1 {
		return errors.New("URI contains more than one fragment delimiter")
	}
	parsed, err := url.Parse(text)
	if err != nil {
		return errors.New("URI cannot be parsed")
	}
	if parsed.Scheme == "" || !uriSchemePattern.MatchString(parsed.Scheme) {
		return errors.New("URI is not absolute")
	}
	return nil
}

func isHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'A' && value <= 'F' || value >= 'a' && value <= 'f'
}

func isRFC3986Character(value byte) bool {
	if value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value >= '0' && value <= '9' {
		return true
	}
	return strings.ContainsRune("-._~:/?#[]@!$&'()*+,;=", rune(value))
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

	schemaErr := e.schemas[selectedVersion].Validate(value)
	structuralCollector := newDiagnosticCollector()
	if schemaErr != nil {
		var validationError *jsonschema.ValidationError
		if errors.As(schemaErr, &validationError) {
			collectStructural(validationError, structuralCollector)
		} else {
			structuralCollector.addError("JPS-STRUCTURE-SCHEMA", "structural", "", "Structural validation failed.")
		}
	}
	extensionNameDiagnostics(root, structuralCollector)
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

	semantic, semanticTruncated := semanticDiagnostics(root)
	if len(semantic) > 0 {
		output.Status = "invalid"
		output.Layers = append(output.Layers, result.Layer{Name: "semantic", Status: "failed"})
		output.Diagnostics = semantic
		output.DiagnosticsTruncated = semanticTruncated
		return output, nil
	}
	output.Layers = append(output.Layers, result.Layer{Name: "semantic", Status: "passed"})

	required := requiredExtensions(root)
	supported := uniqueSorted(options.SupportedExtensions)
	unsupported := difference(required, supported)
	output.Extensions.Required = required
	output.Extensions.Supported = supported
	output.Extensions.Unsupported = unsupported
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
	return output, nil
}

func structuralBootstrapFailure(output result.Validation, location, code, message string) result.Validation {
	output.Status = "invalid"
	output.Layers = append(output.Layers, result.Layer{Name: "structural", Status: "failed"})
	output.Diagnostics = []result.Diagnostic{result.ErrorDiagnostic(code, "structural", location, message)}
	return output
}

func collectStructural(validationError *jsonschema.ValidationError, diagnostics *diagnosticCollector) {
	if diagnostics.full() {
		diagnostics.truncated = true
		return
	}
	location := append([]string(nil), validationError.InstanceLocation...)
	add := func(code string, parts []string, message string) bool {
		return diagnostics.addError(code, "structural", carrier.Pointer(parts), message)
	}
	handled := false

	switch typed := validationError.ErrorKind.(type) {
	case *kind.Required:
		handled = true
		for _, missing := range typed.Missing {
			code := "JPS-STRUCTURE-REQUIRED-MEMBER"
			message := "Required member is missing."
			if contains(location, "exceptions") && (missing == "outcome" || missing == "targetRule") {
				code = "JPS-STRUCTURE-EXCEPTION-SHAPE"
				message = "Exception operands do not match its declared effect."
			}
			if !add(code, append(location, missing), message) {
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
			add(code, location, structuralMessage(code))
		}
	case *kind.MinItems:
		handled = true
		code := "JPS-STRUCTURE-COLLECTION-ARITY"
		if last(location) == "conditions" {
			code = "JPS-STRUCTURE-CONDITION-ARITY"
		} else if last(location) == "triggers" {
			code = "JPS-STRUCTURE-ESCALATION-TRIGGERS"
		}
		add(code, location, structuralMessage(code))
	case *kind.Type:
		handled = true
		code := "JPS-STRUCTURE-TYPE"
		if last(location) == "value" && contains(typed.Want, "array") {
			code = "JPS-STRUCTURE-IN-OPERAND"
		}
		add(code, location, structuralMessage(code))
	case *kind.Const:
		if last(location) == "specVersion" {
			handled = true
			add("JPS-STRUCTURE-SPEC-VERSION", location, "Specification version does not match the selected JPS release.")
		}
	case *kind.Enum:
		handled = true
		add("JPS-STRUCTURE-ENUM", location, "Value is not one of the allowed values.")
	case *kind.MinLength:
		handled = true
		add("JPS-STRUCTURE-MIN-LENGTH", location, "String is shorter than the required minimum length.")
	case *kind.UniqueItems:
		handled = true
		add("JPS-STRUCTURE-UNIQUE-ITEMS", location, "Array items must be unique.")
	case *kind.OneOf:
		handled = true
		directCauses := make([]*jsonschema.ValidationError, 0, len(validationError.Causes))
		for _, cause := range validationError.Causes {
			if _, irrelevantBranch := cause.ErrorKind.(*kind.Group); !irrelevantBranch {
				directCauses = append(directCauses, cause)
			}
		}
		if len(directCauses) == 0 {
			add("JPS-STRUCTURE-CONDITION-SHAPE", location, "Condition does not match one declared condition shape.")
			return
		}
		for _, cause := range directCauses {
			collectStructural(cause, diagnostics)
		}
		return
	case *kind.Not, *kind.AllOf:
		if contains(location, "exceptions") && len(validationError.Causes) == 0 {
			handled = true
			add("JPS-STRUCTURE-EXCEPTION-SHAPE", location, "Exception operands do not match its declared effect.")
		}
	case *kind.Schema, *kind.Reference, *kind.Group:
		handled = true
	}
	if !handled && len(validationError.Causes) == 0 {
		add("JPS-STRUCTURE-SCHEMA", location, "Value does not satisfy a structural constraint.")
	}
	for _, cause := range validationError.Causes {
		collectStructural(cause, diagnostics)
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
		return "Ordered comparison operand is not a valid decimal string."
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
