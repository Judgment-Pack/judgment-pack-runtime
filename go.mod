module github.com/Judgment-Pack/judgment-pack-runtime

// os.Root (Go 1.24) is the containment primitive internal/fssecure is built on:
// every project read is resolved relative to a retained directory handle, which
// is what makes containment hold through the open rather than only up to it
// (ADR-0012). That is the whole reason this floor is 1.24 and not lower.
go 1.24.0

require (
	github.com/dlclark/regexp2 v1.12.0
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	github.com/spf13/cobra v1.10.2
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/text v0.14.0 // indirect
)
