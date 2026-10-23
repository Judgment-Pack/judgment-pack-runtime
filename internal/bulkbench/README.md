# Synthetic bulk evaluation measurement

This developer tool measures actual `Engine.EvaluateAdmitted` calls against a
handwritten synthetic triage pack with four outcomes and twelve rules. It adds no
runtime bulk API. No customer data, Runner, filesystem durability, MCP transport,
or CLI result serialization participates in evaluation. Results are observations
on the reported machine and toolchain, with no performance guarantee.

From the repository root on Linux with procfs mounted, build once and run each
configuration in a fresh process. Choose a writable scratch directory for the
binary and reports. This sequence includes the requested 1000/4096 record sizes
and a deliberate intermediate 2000 record size for checking scaling and noise:

```sh
scratch=$(mktemp -d)
go build -trimpath -o "$scratch/bulkbench" ./internal/bulkbench
git rev-parse HEAD > "$scratch/source-sha.txt"
go version > "$scratch/toolchain.txt"
uname -a > "$scratch/platform.txt"
for records in 1000 4096 2000; do
  for workers in 1 4; do
    GOMAXPROCS=4 "$scratch/bulkbench" -records "$records" -workers "$workers" > "$scratch/n${records}-w${workers}.json" || exit 1
  done
done
```

Run from a stable source commit and record its SHA alongside reports. The fixture
and input SHA-256 hashes identify the exact embedded bytes. `workers=1` runs a
serial loop; larger values launch exactly that many workers, statically assigning
record indices by stride. The limit is `1 <= workers <= min(records, 1024)`.
Worker concurrency and `GOMAXPROCS` are separate: record both. Platform, Go
version, logical CPU count and actual `GOMAXPROCS` appear in every JSON report;
record CPU model and available container/VM CPU and memory limits alongside them
when comparing machines.

Each record uses `inputs[i % 12]`. The input file states expected outcomes by
hand, independently of evaluator results. Every result must be an outcome with
that ID; any refusal or mismatch fails the command without a timing report. The
completed count must equal N and every outcome count must match the same input
oracle. Thus serial and parallel runs validate against identical expectations,
including N values that are not multiples of twelve. For 1000 records the counts
are proceed=252, clarify=250, decline=249, specialist=249; for 4096 they are
proceed=1026, clarify=1024, decline=1023, specialist=1023. For 2000 they are
proceed=501, clarify=501, decline=500, specialist=498.

Timing fields are nanoseconds, measured with Go's monotonic clock:

- `setup_ns` covers input JSON preparation and bundled validation-engine/schema
  initialization. Compilation of the tool happens in the earlier build command.
- `admission_and_warmup_ns` covers `AdmitPack` and one evaluation of all twelve
  handwritten input rows. `AdmitPack` snapshots lazily; the first warmup evaluation
  forces full conformance admission into its cache. All timed records reuse that
  same admitted pack and the same evaluation options.
- `evaluation_ns` is elapsed wall time for N evaluations, including worker launch
  and join, each record's normal facts preflight/decoding, result construction,
  outcome checks and aggregation/completion validation. No input/pack generation
  or admission is in this interval. `evaluation_ns_per_record` divides that wall
  time by N; it is amortized elapsed time, not a sampled latency distribution.
  Parallel runs overlap calls, so this is not individual request latency.

`process_lifetime_peak_rss_bytes` is Linux `/proc/self/status` **VmHWM**, read after
completion and converted from kernel kB units to bytes (1024 bytes per kB). It is
the kernel's process-lifetime resident-set high-water mark, including input/schema
setup, admission, warmup, evaluation, stacks and resident runtime overhead. It is
not an evaluation-only peak, heap peak, heap live size, or total allocation count.
It excludes the earlier compiler process and the invoking shell. The read occurs
before report serialization; this field does not account for subsequent output
work. No heap or allocation metric is substituted for a missing RSS metric. On
platforms without Linux procfs the package still compiles and its correctness
tests run, but invoking the measurement command fails clearly.

Compare total time, amortized per-record time and RSS for each mode across sizes.
A ratio need not equal the size ratio: scheduler load, GC, worker startup and
measurement noise contribute. RSS includes substantial fixed setup and can stay
flat across these small workloads. A single sequence demonstrates observed
scaling/noise, not a complexity proof or capacity guarantee. There are no CI
assertions on timing or memory values. Focused correctness checks:

```sh
go test ./internal/bulkbench
go test -race ./internal/bulkbench
```
