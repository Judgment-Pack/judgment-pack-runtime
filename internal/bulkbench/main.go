// bulkbench is developer measurement tooling, not a runtime bulk API.
package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

//go:embed testdata/triage.json
var pack []byte

//go:embed testdata/inputs.json
var inputs []byte

type input struct {
	Facts   json.RawMessage `json:"facts"`
	Outcome string          `json:"outcome"`
}

type tally struct {
	Completed      int            `json:"completed"`
	Counts         map[string]int `json:"outcome_counts"`
	ExpectedCounts map[string]int `json:"expected_outcome_counts"`
}

type report struct {
	Records      int     `json:"records"`
	Workers      int     `json:"workers"`
	Mode         string  `json:"mode"`
	Platform     string  `json:"platform"`
	Toolchain    string  `json:"toolchain"`
	CPUs         int     `json:"logical_cpus"`
	GOMAXPROCS   int     `json:"gomaxprocs"`
	PackSHA256   string  `json:"pack_sha256"`
	InputsSHA256 string  `json:"inputs_sha256"`
	SetupNS      int64   `json:"setup_ns"`
	AdmissionNS  int64   `json:"admission_and_warmup_ns"`
	EvaluationNS int64   `json:"evaluation_ns"`
	PerRecordNS  float64 `json:"evaluation_ns_per_record"`
	PeakRSSBytes uint64  `json:"process_lifetime_peak_rss_bytes"`
	MemoryMetric string  `json:"memory_metric"`
	tally
}

const maxWorkers = 1024

type evaluate func([]byte) (result.Evaluation, *evaluation.Failure)

func check(row input, evaluated result.Evaluation, failure *evaluation.Failure) error {
	if failure != nil {
		return fmt.Errorf("evaluation refused: %+v", failure)
	}
	if evaluated.Disposition.Kind != "outcome" || evaluated.Disposition.OutcomeID != row.Outcome {
		return fmt.Errorf("expected outcome %q; got %+v", row.Outcome, evaluated.Disposition)
	}
	return nil
}

// Only worker-local counters are mutated while evaluating. At most workers
// goroutines exist, and each index belongs to exactly one worker.
func run(rows []input, n, workers int, eval evaluate) (tally, error) {
	if len(rows) == 0 || n < 1 || workers < 1 || workers > n || workers > maxWorkers {
		return tally{}, fmt.Errorf("require inputs and 1 <= workers <= min(records, 1024)")
	}
	results := make([]tally, workers)
	failures := make([]error, workers)
	work := func(w int) {
		counts := map[string]int{}
		for i := w; i < n; i += workers {
			row := rows[i%len(rows)]
			evaluated, failure := eval(row.Facts)
			if err := check(row, evaluated, failure); err != nil {
				failures[w] = fmt.Errorf("record %d: %w", i, err)
				return
			}
			counts[evaluated.Disposition.OutcomeID]++
			results[w].Completed++
		}
		results[w].Counts = counts
	}
	if workers == 1 {
		work(0)
	} else {
		var wg sync.WaitGroup
		for w := range workers {
			wg.Add(1)
			go func() { defer wg.Done(); work(w) }()
		}
		wg.Wait()
	}
	total := tally{Counts: map[string]int{}}
	for w, r := range results {
		if failures[w] != nil {
			return tally{}, failures[w]
		}
		total.Completed += r.Completed
		for outcome, count := range r.Counts {
			total.Counts[outcome] += count
		}
	}
	expected := map[string]int{}
	for i := 0; i < n; i++ {
		expected[rows[i%len(rows)].Outcome]++
	}
	if total.Completed != n || len(expected) != len(total.Counts) {
		return tally{}, fmt.Errorf("completion/count mismatch: %+v", total)
	}
	for outcome, count := range expected {
		if total.Counts[outcome] != count {
			return tally{}, fmt.Errorf("outcome %s: expected %d, got %d", outcome, count, total.Counts[outcome])
		}
	}
	total.ExpectedCounts = expected
	return total, nil
}

func peakRSS() (uint64, error) {
	// Linux VmHWM is the kernel's process-lifetime resident-set high-water mark.
	// It includes setup and admission. It is neither Go heap nor allocation total.
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, fmt.Errorf("peak RSS requires Linux /proc/self/status: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "VmHWM:" && fields[2] == "kB" {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			return value * 1024, err
		}
	}
	return 0, fmt.Errorf("Linux VmHWM unavailable")
}

func measure(n, workers int) (report, error) {
	if n < 1 || workers < 1 || workers > n || workers > maxWorkers {
		return report{}, fmt.Errorf("require 1 <= workers <= min(records, 1024)")
	}
	if _, err := peakRSS(); err != nil {
		return report{}, err
	}
	start := time.Now()
	var rows []input
	if err := json.Unmarshal(inputs, &rows); err != nil {
		return report{}, err
	}
	validator, err := validation.NewEngine()
	if err != nil {
		return report{}, err
	}
	engine := evaluation.NewEngine(validator)
	r := report{Records: n, Workers: workers, Mode: "parallel", Platform: runtime.GOOS + "/" + runtime.GOARCH, Toolchain: runtime.Version(), CPUs: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0), PackSHA256: fmt.Sprintf("%x", sha256.Sum256(pack)), InputsSHA256: fmt.Sprintf("%x", sha256.Sum256(inputs)), MemoryMetric: "Linux /proc/self/status VmHWM * 1024; process lifetime including setup/admission; excludes compiler and parent"}
	if workers == 1 {
		r.Mode = "serial"
	}
	r.SetupNS = time.Since(start).Nanoseconds()
	start = time.Now()
	admitted := engine.AdmitPack(pack)
	eval := func(facts []byte) (result.Evaluation, *evaluation.Failure) {
		return engine.EvaluateAdmitted(admitted, facts, nil, evaluation.Options{Command: "bulkbench"})
	}
	// AdmitPack snapshots lazily: the first evaluation performs cached conformance
	// admission. Warm every handwritten row before starting either timed mode.
	for _, row := range rows {
		evaluated, failure := eval(row.Facts)
		if err := check(row, evaluated, failure); err != nil {
			return report{}, fmt.Errorf("admission/warmup: %w", err)
		}
	}
	r.AdmissionNS = time.Since(start).Nanoseconds()
	start = time.Now()
	r.tally, err = run(rows, n, workers, eval)
	r.EvaluationNS = time.Since(start).Nanoseconds()
	if err != nil {
		return report{}, err
	}
	r.PerRecordNS = float64(r.EvaluationNS) / float64(n)
	r.PeakRSSBytes, err = peakRSS()
	return r, err
}

func main() {
	n := flag.Int("records", 1000, "synthetic records (cycle handwritten inputs in order)")
	workers := flag.Int("workers", 1, "bounded concurrency; 1 selects serial")
	flag.Parse()
	r, err := measure(*n, *workers)
	if err == nil {
		err = json.NewEncoder(os.Stdout).Encode(r)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
