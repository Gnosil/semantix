package cli

import (
	"time"

	"semantix/harness/internal/control"
	"semantix/harness/internal/memory"
)

func renderMemory(width int, set *memory.Set) string {
	return viewProtectLines(control.RenderMemorySummary(set, time.Now().UTC()), width)
}
