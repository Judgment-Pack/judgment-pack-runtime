package cli

import (
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// renderStrength prints one pack's test-strength run (ADR-0084): its counts,
// then each survivor with the change in the pack's own words and the hint of
// a row that would notice it. Noticed and not-derivable variants are counted
// here and listed only in the JSON payload; a survivor is the line a reader
// acts on. Nothing here prints a row's facts.
func (a *App) renderStrength(indent string, strength *result.PackStrength) {
	if strength == nil {
		return
	}
	if strength.Status != result.StrengthRan {
		fmt.Fprintf(a.out, "%sstrength: skipped: %s\n", indent, display.Sanitize(strength.Detail))
		return
	}
	fmt.Fprintf(a.out, "%sstrength: %s\n", indent, strengthCountsLine(strength.Counts))
	for _, variant := range strength.Variants {
		if variant.Status != result.VariantSurvived {
			continue
		}
		fmt.Fprintf(a.out, "%s  survived %s (%s): %s\n", indent, display.Sanitize(variant.ID), display.Sanitize(variant.Declaration), display.Sanitize(variant.Change))
		if variant.Hint != "" {
			fmt.Fprintf(a.out, "%s    hint: %s\n", indent, display.Sanitize(variant.Hint))
		}
	}
}

func strengthCountsLine(counts result.StrengthCounts) string {
	variants, changes := "variants", "changes"
	if counts.Derived == 1 {
		variants = "variant"
	}
	if counts.NotDerivable == 1 {
		changes = "change"
	}
	return fmt.Sprintf("%d %s derived, %d noticed, %d survived; %d %s not derivable",
		counts.Derived, variants, counts.Noticed, counts.Survived, counts.NotDerivable, changes)
}
