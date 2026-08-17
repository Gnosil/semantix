package boot

import (
	"semantix/harness/internal/agent"
	"semantix/harness/internal/history"
)

func newObservedSession(systemPrompt string) *agent.Session {
	session := agent.NewSession(systemPrompt)
	session.SetPersistObserver(history.PersistObserver())
	return session
}
