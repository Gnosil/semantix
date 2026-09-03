package serve

import (
	"encoding/json"
	"sync"
	"time"

	"semantix/harness/billing"
	"semantix/harness/event"
	"semantix/harness/eventwire"
)

// webHistoryLimit bounds the protocol data retained for an SSE reconnect.
// The raw /events stream remains live-only; this history is only for the
// versioned /workspace/events transport.
const webHistoryLimit = 256

// webFrame is the protocol-facing representation of one forwarded event. The
// sequence is assigned once at publish time so every workspace connection
// observes the same ordering and Last-Event-ID has stable meaning.
type webFrame struct {
	seq    uint64
	type_  string
	timeMS int64
	data   []byte
}

type webDelivery struct {
	frame  webFrame
	taskID string
}

type webSubscriber struct {
	ch     chan webDelivery
	taskID string
}

// Broadcaster is the event.Sink the controller emits to in server mode. It
// marshals each event once and fans it out to every connected SSE subscriber.
// A slow subscriber's buffer is allowed to drop rather than back-pressure the
// agent goroutine — a browser that can't keep up loses intermediate frames, not
// the whole session (it can refetch /history).
type Broadcaster struct {
	mu              sync.Mutex
	subs            map[chan []byte]struct{}
	webSubs         map[*webSubscriber]struct{}
	webHistory      []webFrame
	webSeq          uint64
	ledger          *billing.Ledger
	displayCurrency string
}

// NewBroadcaster returns an empty Broadcaster ready to accept subscribers.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subs:    map[chan []byte]struct{}{},
		webSubs: map[*webSubscriber]struct{}{},
		ledger:  billing.NewLedger(),
	}
}

// SetDisplayCurrency rebinds the session ledger to a stored valuation. Empty
// keeps automatic mode: a single original currency is selected and mixed
// currencies remain buckets.
func (b *Broadcaster) SetDisplayCurrency(currency string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.displayCurrency = billing.NormalizeCurrency(currency)
	b.mu.Unlock()
}

// ResetSession clears the usage ledger for /new, /resume and /fork. Workspace
// replay is session-scoped, so old protocol history and subscribers are closed
// as well; EventSource clients reconnect with the new task id instead of
// receiving frames from the previous session.
func (b *Broadcaster) ResetSession() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.ledger = billing.NewLedger()
	b.webHistory = nil
	for sub := range b.webSubs {
		delete(b.webSubs, sub)
		close(sub.ch)
	}
	b.mu.Unlock()
}

// SessionCostQuote returns the current aggregate quote without repricing.
func (b *Broadcaster) SessionCostQuote() billing.CostQuote {
	if b == nil {
		return billing.AggregateQuotes(nil, "")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ledger == nil {
		b.ledger = billing.NewLedger()
	}
	return b.ledger.Total(b.displayCurrency)
}

// Emit marshals the event to JSON and delivers it to every subscriber. Drops to
// a subscriber whose buffer is full rather than blocking. A marshal failure is
// dropped silently — one bad event shouldn't stall the stream.
func (b *Broadcaster) Emit(e event.Event) {
	data, err := json.Marshal(eventwire.ToWire(e))
	if err != nil {
		return
	}
	protoType, keep, _ := classifyFrame(data)
	b.mu.Lock()
	defer b.mu.Unlock()
	if e.Kind == event.Usage && e.Usage != nil && e.CostQuote != nil {
		if b.ledger == nil {
			b.ledger = billing.NewLedger()
		}
		b.ledger.Add(*e.CostQuote, billing.UsageTokens{
			PromptTokens: e.Usage.PromptTokens, CompletionTokens: e.Usage.CompletionTokens,
			CacheHitTokens: e.Usage.CacheHitTokens, CacheMissTokens: e.Usage.CacheMissTokens,
			CacheWriteTokens: e.Usage.CacheWriteTokens, CacheWriteBilledTokens: e.Usage.CacheWriteBilledTokens,
			Estimated: e.Usage.Estimated,
		}, time.Now().UTC())
	}
	for ch := range b.subs {
		select {
		case ch <- data:
		default: // subscriber is behind; drop this frame for it
		}
	}
	if keep {
		frame := b.publishWebFrame(protoType, data)
		for sub := range b.webSubs {
			select {
			case sub.ch <- webDelivery{frame: frame, taskID: sub.taskID}:
			default: // preserve agent liveness; clients can reconnect on a gap
			}
		}
	}
}

// EmitTo delivers an event only to the supplied subscriber. It is used for
// connection-local recovery frames, such as replaying a prompt to a browser
// that attached after the original event was emitted. Normal runtime events
// should continue to use Emit so every subscriber receives them.
func (b *Broadcaster) EmitTo(target <-chan []byte, e event.Event) {
	data, err := json.Marshal(eventwire.ToWire(e))
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		if (<-chan []byte)(ch) != target {
			continue
		}
		select {
		case ch <- data:
		default: // subscriber is behind; drop this frame rather than blocking.
		}
		return
	}
}

// EmitWebTo delivers a real event to one workspace subscriber without adding a
// target-only replay to the shared history. This is used for pending approval
// and ask prompts, which are re-emitted only to the newly attached client.
func (b *Broadcaster) EmitWebTo(target <-chan webDelivery, taskID string, e event.Event) {
	data, err := json.Marshal(eventwire.ToWire(e))
	if err != nil {
		return
	}
	protoType, keep, _ := classifyFrame(data)
	if !keep {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.webSubs {
		if (<-chan webDelivery)(sub.ch) != target {
			continue
		}
		frame := b.newWebFrame(protoType, data)
		select {
		case sub.ch <- webDelivery{frame: frame, taskID: taskID}:
		default:
		}
		return
	}
}

// publishWebFrame assigns a stable sequence and appends one protocol frame to
// the bounded replay log. b.mu must be held by the caller.
func (b *Broadcaster) publishWebFrame(protoType string, data []byte) webFrame {
	frame := b.newWebFrame(protoType, data)
	b.webHistory = append(b.webHistory, frame)
	if excess := len(b.webHistory) - webHistoryLimit; excess > 0 {
		copy(b.webHistory, b.webHistory[excess:])
		b.webHistory = b.webHistory[:webHistoryLimit]
	}
	return frame
}

// newWebFrame allocates a sequence without retaining the frame. b.mu must be
// held by the caller.
func (b *Broadcaster) newWebFrame(protoType string, data []byte) webFrame {
	b.webSeq++
	return webFrame{
		seq:    b.webSeq,
		type_:  protoType,
		timeMS: time.Now().UnixMilli(),
		data:   append([]byte(nil), data...),
	}
}

// Subscribe registers a new SSE client and returns its channel plus an
// unsubscribe func the handler must call (defer) when the client disconnects.
func (b *Broadcaster) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
}

// SubscribeWebSince atomically registers a workspace subscriber and snapshots
// all retained frames after lastSeq. gap is true when the requested sequence
// predates the retained window; callers should replay the available suffix and
// refresh derived state from /history.
func (b *Broadcaster) SubscribeWebSince(lastSeq uint64, taskID string) (<-chan webDelivery, []webDelivery, bool, func()) {
	sub := &webSubscriber{ch: make(chan webDelivery, 64), taskID: taskID}
	b.mu.Lock()
	defer b.mu.Unlock()

	gap := false
	if lastSeq > 0 {
		if len(b.webHistory) == 0 || lastSeq < b.webHistory[0].seq-1 {
			gap = true
		}
	}
	replay := make([]webDelivery, 0, len(b.webHistory))
	for _, frame := range b.webHistory {
		if frame.seq > lastSeq {
			replay = append(replay, webDelivery{frame: frame, taskID: taskID})
		}
	}
	b.webSubs[sub] = struct{}{}
	return sub.ch, replay, gap, func() {
		b.mu.Lock()
		if _, ok := b.webSubs[sub]; ok {
			delete(b.webSubs, sub)
			close(sub.ch)
		}
		b.mu.Unlock()
	}
}

// ReplayWindow snapshots the broadcaster's retained workspace frames for a
// client that attached after they were emitted (#403). It is the JSON sibling
// of the SubscribeWebSince SSE path — the same bounded window, same ordering,
// no live subscription. taskID is stamped onto every delivery exactly like a
// fresh subscriber so the replay frames carry the same task attribution as the
// live stream. Frames are immutable after publish, so sharing them is safe.
func (b *Broadcaster) ReplayWindow(taskID string) []webDelivery {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	replay := make([]webDelivery, 0, len(b.webHistory))
	for _, frame := range b.webHistory {
		replay = append(replay, webDelivery{frame: frame, taskID: taskID})
	}
	return replay
}

// SubscribeWebLive registers a workspace subscriber without replaying retained
// frames. The workspace uses this while it hydrates the durable /history
// snapshot, buffering any events that arrive during that request.
func (b *Broadcaster) SubscribeWebLive(taskID string) (<-chan webDelivery, func()) {
	sub := &webSubscriber{ch: make(chan webDelivery, 64), taskID: taskID}
	b.mu.Lock()
	b.webSubs[sub] = struct{}{}
	b.mu.Unlock()
	return sub.ch, func() {
		b.mu.Lock()
		if _, ok := b.webSubs[sub]; ok {
			delete(b.webSubs, sub)
			close(sub.ch)
		}
		b.mu.Unlock()
	}
}

// Subscribers reports the current connection count (for diagnostics/tests).
func (b *Broadcaster) Subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
