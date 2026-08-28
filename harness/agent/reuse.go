package agent

import (
	"encoding/json"
	"time"

	"semantix/harness/event"
	"semantix/harness/semantix"
)

// reuseNotice builds the per-turn reuse panel Notice (U33/H4a). Returns ok
// false when there is nothing to show: the panel only fires when the kernel
// reported at least one hit slice, so a kernel-less or hit-less turn adds
// zero noise to the transcript.
func reuseNotice(reuse semantix.ReuseSummary) (event.Event, bool) {
	if reuse.Hits <= 0 {
		return event.Event{}, false
	}
	detail, err := json.Marshal(reuse)
	if err != nil {
		return event.Event{}, false
	}
	return event.Event{
		Kind:   event.Notice,
		Level:  event.LevelInfo,
		Code:   event.NoticeCodeSemantixReuse,
		Text:   reuse.Line(),
		Detail: string(detail),
	}, true
}

// emitReuse publishes the turn's reuse panel after the turn settles. It runs
// on every Run return path (success, error, cancellation) — the data was
// already gathered at turn start, so a cancelled turn still reports what it
// would have reused.
func (a *Agent) emitReuse(state *turnRuntime) {
	if state == nil {
		return
	}
	// U38 task completion time: attach the current task's elapsed/completed
	// time to the panel payload before it is rendered. No task clock yet (the
	// first run started without a scoped task) degrades to an omitted segment.
	if !a.task.taskStartedAt.IsZero() {
		if a.task.taskCompletedAt != nil {
			state.reuse.TaskElapsedSeconds = a.task.taskCompletedAt.Sub(a.task.taskStartedAt).Seconds()
			state.reuse.TaskComplete = true
		} else {
			state.reuse.TaskElapsedSeconds = time.Since(a.task.taskStartedAt).Seconds()
		}
	}
	if notice, ok := reuseNotice(state.reuse); ok {
		a.svc.sink.Emit(notice)
	}
	// Seal the kernel session mirror's turn on every Run return path:
	// synchronous runs never emit TurnDone, and headless processes can exit
	// without reaching the bridge's deferred Close.
	a.semantix.EndTurn()
}
