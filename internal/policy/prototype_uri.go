package policy

import (
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// prototypeURIFormat preserves the historical 0.0.2 companion assertion, also
// for its retained Core consumers. It is compatibility code, not an RFC parser.
// Ordinary Core and official Policy validation use internal/uriformat instead.
func prototypeURIFormat() *jsonschema.Format {
	return &jsonschema.Format{Name: "uri", Validate: validatePrototypeURI}
}

var uriSchemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*$`)

func validatePrototypeURI(value any) error {
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
