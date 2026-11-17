// The golden module of the Go typed loader emitter (ADR-0102): its vectors'
// expected files, and a copy of one vector's files with a decode test. The
// generated code imports the start-up module, which the root module does not
// require, so it is compiled here, against the start-up module of this
// repository through a local replace.
module github.com/Judgment-Pack/judgment-pack-runtime/cmd/jpack-emit-go-loader/golden

go 1.24.0

require github.com/Judgment-Pack/judgment-pack-runtime/startup v0.0.0-00010101000000-000000000000

replace github.com/Judgment-Pack/judgment-pack-runtime/startup => ../../../startup
