package policy

import (
	"errors"
	"net/netip"
	"strings"
)

// Candidate URI assertions follow the components in RFC 3986 Appendix A,
// https://www.rfc-editor.org/rfc/rfc3986#appendix-A. This is generic URI syntax,
// with no scheme-specific network, DNS, port-range or publication semantics.
// Core/prototype schemas retain their existing URI predicate.
func validateCandidateURI(value any) error {
	uri, ok := value.(string)
	if !ok {
		return nil
	}
	if !candidateURI(uri) {
		return errors.New("URI does not satisfy the candidate URI format assertion")
	}
	return nil
}

func candidateURI(uri string) bool {
	scheme, rest, found := strings.Cut(uri, ":")
	if !found || scheme == "" || !uriAlpha(scheme[0]) {
		return false
	}
	for i := 1; i < len(scheme); i++ {
		if !uriAlpha(scheme[i]) && !uriDigit(scheme[i]) && !strings.ContainsRune("+.-", rune(scheme[i])) {
			return false
		}
	}
	// Query and fragment allow pchar, slash and question mark. A second raw
	// fragment delimiter is consequently refused by the component grammar.
	if body, fragment, found := strings.Cut(rest, "#"); found {
		if !uriComponent(fragment, ":@/?", true) {
			return false
		}
		rest = body
	}
	if body, query, found := strings.Cut(rest, "?"); found {
		if !uriComponent(query, ":@/?", true) {
			return false
		}
		rest = body
	}
	if strings.HasPrefix(rest, "//") {
		authority, path, found := strings.Cut(rest[2:], "/")
		if !uriAuthority(authority) {
			return false
		}
		if !found {
			return true
		}
		return uriComponent(path, ":@/", true)
	}
	// Without an authority, a leading double slash is unavailable. The three
	// remaining path alternatives are absolute, rootless and empty; each uses
	// the same pchar/segment grammar once authority selection is settled.
	return uriComponent(rest, ":@/", true)
}

func uriAuthority(authority string) bool {
	if userinfo, host, found := strings.Cut(authority, "@"); found {
		if !uriComponent(userinfo, ":", true) {
			return false
		}
		authority = host
	}
	if strings.HasPrefix(authority, "[") {
		literal, suffix, found := strings.Cut(authority[1:], "]")
		if !found || !uriIPLiteral(literal) {
			return false
		}
		return suffix == "" || strings.HasPrefix(suffix, ":") && uriPort(suffix[1:])
	}
	host, port, found := strings.Cut(authority, ":")
	// reg-name includes the textual IPv4 alternative and permits percent
	// escapes in host names. It is not restricted to DNS label syntax.
	if !uriComponent(host, "", true) {
		return false
	}
	return !found || uriPort(port)
}

func uriPort(port string) bool {
	for i := range port {
		if !uriDigit(port[i]) {
			return false
		}
	}
	return true
}

func uriIPLiteral(literal string) bool {
	if strings.HasPrefix(literal, "v") || strings.HasPrefix(literal, "V") {
		version, address, found := strings.Cut(literal[1:], ".")
		if !found || version == "" || address == "" {
			return false
		}
		for i := range version {
			if !uriHex(version[i]) {
				return false
			}
		}
		return uriComponent(address, ":", false)
	}
	// Scoped addresses are a later extension, outside the candidate's exact
	// RFC 3986 assertion; a plain IPv4 address cannot occupy IP-literal.
	address, err := netip.ParseAddr(literal)
	return err == nil && address.Is6() && address.Zone() == ""
}

func uriComponent(value, extra string, escapes bool) bool {
	allowed := "-._~!$&'()*+,;=" + extra
	for i := 0; i < len(value); i++ {
		c := value[i]
		if escapes && c == '%' {
			if i+2 >= len(value) || !uriHex(value[i+1]) || !uriHex(value[i+2]) {
				return false
			}
			i += 2
			continue
		}
		if !uriAlpha(c) && !uriDigit(c) && !strings.ContainsRune(allowed, rune(c)) {
			return false
		}
	}
	return true
}

func uriAlpha(c byte) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }
func uriDigit(c byte) bool { return c >= '0' && c <= '9' }
func uriHex(c byte) bool   { return uriDigit(c) || c >= 'A' && c <= 'F' || c >= 'a' && c <= 'f' }
