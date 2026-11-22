import { execFile } from 'node:child_process'
import { homedir } from 'node:os'
import { join } from 'node:path'

export const name = 'semantix-dsh-memory'

const inputLimit = 1024 * 1024
const source = { kind: 'plugin', plugin: name }

function textOf(messages) {
  return messages.flatMap(message => message.content)
    .filter(block => block.type === 'text').map(block => block.text).join('\n')
}

function hook(session, turn, event, field, text, signal) {
  const input = JSON.stringify({
    hook_event_name: event,
    cwd: session.header.cwd,
    session_id: session.header.id,
    turn_id: String(turn),
    [field]: text,
  })
  if (Buffer.byteLength(input) > inputLimit || signal?.aborted) return Promise.resolve()

  return new Promise(resolve => {
    const child = execFile('semantix', ['codex-hook'], {
      env: { ...process.env, PLUGIN_DATA: process.env.SEMANTIX_DSH_DATA_DIR || join(homedir(), '.semantix', 'dsh') },
      timeout: event === 'Stop' ? 5000 : 2000,
      maxBuffer: 32 * 1024,
      windowsHide: true,
      signal,
    }, (error, stdout) => {
      if (!error) {
        try {
          const context = JSON.parse(stdout).hookSpecificOutput
          if (context?.hookEventName === event && typeof context.additionalContext === 'string') {
            resolve(context.additionalContext)
            return
          }
        } catch {
          // A missing or incompatible CLI must not interrupt the agent.
        }
      }
      resolve(undefined)
    })
    child.stdin.on('error', () => {})
    child.stdin.end(input)
  }).catch(() => undefined)
}

export function apply(ctx) {
  const active = new WeakMap()
  const writes = new WeakMap()

  ctx.on('agent/pre-step', async ({ agent, turn, step, signal }, next) => {
    const decision = await next()
    if (step !== 1 || decision.kind !== 'enter') return decision
    const prompt = textOf(decision.messages.filter(message => message.source?.kind === 'user'))
    if (!prompt.trim()) return decision

    const session = agent.session
    const context = await hook(session, turn, 'UserPromptSubmit', 'prompt', prompt, signal)
    if (signal.aborted) return decision
    active.set(session, { turn, response: '' })
    if (!context) return decision
    const memory = { role: 'user', source, content: [{ type: 'text', text: context }] }
    return { ...decision, messages: [...decision.messages, memory] }
  })

  ctx.on('session/event', (session, event) => {
    const current = active.get(session)
    if (!current || event.data?.turn !== current.turn) return
    if (event.type === 'assistant/message') {
      const message = event.data.message
      current.response = message.content.some(block => block.type === 'tool-call') ? '' : textOf([message])
    } else if (event.type === 'turn/end') {
      active.delete(session)
      const response = event.data.reason.kind === 'completed' ? current.response : ''
      const write = Promise.resolve(writes.get(session)).then(() =>
        hook(session, current.turn, 'Stop', 'last_assistant_message', response))
      writes.set(session, write)
      void write.finally(() => {
        if (writes.get(session) === write) writes.delete(session)
      })
    }
  })

  // Headless DSH flushes the session before it requests process exit.
  ctx.on('session/flush', session => writes.get(session))
}
