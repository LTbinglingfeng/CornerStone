import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { fileURLToPath } from 'node:url'
import { build } from 'vite'

// Compile the actual hook, just as qqOfficialSettings.test.mjs compiles its component.
const hookPath = fileURLToPath(new URL('../src/components/ChatDetail/hooks/useMessageStream.ts', import.meta.url))
const compiled = await build({
    configFile: false,
    logLevel: 'silent',
    build: {
        write: false,
        minify: false,
        ssr: hookPath,
        rolldownOptions: {
            external: id => id !== hookPath,
            output: { format: 'cjs', exports: 'named' },
        },
    },
})
assert(!Array.isArray(compiled))
const code = compiled.output.find(entry => entry.type === 'chunk' && entry.isEntry).code

const toolMessage = timestamp => ({
    role: 'assistant', content: '', timestamp,
    tool_calls: [{ id: 'reused-call-id', type: 'function', function: { name: 'get_time', arguments: '{}' } }],
})

function harness() {
    const historical = toolMessage('history')
    const slots = [], effects = [], requests = [], errors = [], timers = new Map()
    let cursor = 0, dirty = true, current, sessionId = 'session-a', messages = [historical], timerId = 0
    const changed = (before, after) => !before || !after || before.length !== after.length ||
        after.some((value, i) => !Object.is(value, before[i]))
    const react = {
        useState(initial) {
            const i = cursor++
            if (!(i in slots)) slots[i] = typeof initial === 'function' ? initial() : initial
            return [slots[i], next => {
                const value = typeof next === 'function' ? next(slots[i]) : next
                if (!Object.is(value, slots[i])) { slots[i] = value; dirty = true }
            }]
        },
        useRef(value) { const i = cursor++; return slots[i] ??= { current: value } },
        useCallback(fn, deps) {
            const i = cursor++
            if (changed(slots[i]?.deps, deps)) slots[i] = { fn, deps }
            return slots[i].fn
        },
        useEffect(fn, deps) {
            const i = cursor++
            if (changed(slots[i]?.deps, deps)) {
                const previous = slots[i]
                slots[i] = { deps }
                effects.push(() => { previous?.cleanup?.(); slots[i].cleanup = fn() })
            }
        },
    }
    const setMessages = next => { messages = typeof next === 'function' ? next(messages) : next; dirty = true }
    const onError = error => errors.push(error)
    const reset = () => {}
    const modules = {
        react,
        '../../../i18n': { translate: key => key },
        '../../../utils/replyWaitWindow': { getReplyWaitWindowConfig: () => ({ mode: 'fixed', seconds: 60 }) },
        './useMessageReveal': {
            // Keep animation timing outside this regression; finish reveal on the next effect.
            useMessageReveal(options) {
                react.useEffect(() => {
                    if (options.assistantResponseDone) options.onSendingFinished()
                }, [options.assistantResponseDone])
                return { reset }
            },
        },
        '../../../services/api': {
            sendMessageBeacon() { assert.fail('unexpected beacon') },
            async sendMessage(id, payload, options) {
                const queue = []
                let pending, failure
                const reader = {
                    read() {
                        if (failure) return Promise.reject(failure)
                        if (queue.length) return Promise.resolve(queue.shift())
                        return new Promise((resolve, reject) => { pending = { resolve, reject } })
                    },
                }
                const push = value => {
                    if (pending) { const waiter = pending; pending = undefined; waiter.resolve(value) }
                    else queue.push(value)
                }
                const request = {
                    id, payload, signal: options.signal,
                    event(value) {
                        const data = typeof value === 'string' ? value : JSON.stringify(value)
                        push({ done: false, value: new TextEncoder().encode(`data: ${data}\n\n`) })
                    },
                    end() { push({ done: true }) },
                }
                options.signal.addEventListener('abort', () => {
                    failure = new DOMException('Aborted', 'AbortError')
                    pending?.reject(failure)
                    pending = undefined
                }, { once: true })
                requests.push(request)
                return { headers: { get: () => 'text/event-stream' }, body: { getReader: () => reader } }
            },
        },
    }
    const module = { exports: {} }
    const events = { addEventListener() {}, removeEventListener() {} }
    vm.runInNewContext(code, {
        module, exports: module.exports,
        require: id => { assert(id in modules, `unexpected import: ${id}`); return modules[id] },
        AbortController, DOMException, TextDecoder, console,
        window: {
            ...events,
            setTimeout(fn) { timers.set(++timerId, fn); return timerId },
            clearTimeout(id) { timers.delete(id) },
        },
        document: { ...events, visibilityState: 'visible' },
    })
    function render() {
        let passes = 0
        do {
            assert(++passes < 30, 'hook render did not settle')
            dirty = false
            cursor = 0
            current = module.exports.useMessageStream({ sessionId, messages, setMessages, onError })
            effects.splice(0).forEach(effect => effect())
        } while (dirty)
        return current
    }
    async function settle() {
        for (let i = 0; i < 12; i++) { await Promise.resolve(); render() }
    }
    render()
    return {
        historical, errors, requests, render, settle,
        get messages() { return messages },
        async start() {
            await render().sendMessage({ role: 'user', content: 'hello', timestamp: 'user' })
            const completion = render().flushPendingMessages('foreground')
            await settle()
            assert.equal(render().sending, true)
            return { request: requests.at(-1), completion }
        },
        async message(request, message) { request.event({ type: 'message', message }); await settle() },
        switchSession() { sessionId = 'session-b'; messages = [toolMessage('other-history')]; render() },
    }
}

test('only the current assistant tool message is active, even with reused call IDs', async () => {
    const h = harness()
    assert.equal(h.render().activeToolCallMessage, null)
    const { request, completion } = await h.start()
    assert.equal(h.render().activeToolCallMessage, null)
    await h.message(request, toolMessage('first'))
    const first = h.messages.at(-1)
    assert.equal(h.render().activeToolCallMessage, first)
    assert.notEqual(first, h.historical)
    await h.message(request, { role: 'tool', tool_call_id: 'reused-call-id', content: 'ok', timestamp: 'result' })
    assert.equal(h.render().activeToolCallMessage, first)
    await h.message(request, toolMessage('second'))
    assert.equal(h.render().activeToolCallMessage, h.messages.at(-1))
    assert.notEqual(h.render().activeToolCallMessage, first)
    await h.message(request, { role: 'assistant', content: 'finished', timestamp: 'final' })
    assert.equal(h.render().activeToolCallMessage, null)
    request.event('[DONE]')
    await completion
    await h.settle()
    assert.equal(h.render().activeToolCallMessage, null)
})

for (const ending of ['DONE', 'EOF', 'error', 'abort']) {
    test(`${ending} clears active tools and the next request never activates history`, async () => {
        const h = harness()
        const { request, completion } = await h.start()
        await h.message(request, toolMessage('current'))
        const previous = h.messages.at(-1)
        assert.equal(h.render().activeToolCallMessage, previous)
        if (ending === 'DONE') request.event('[DONE]')
        if (ending === 'EOF') request.end()
        if (ending === 'error') request.event({ error: 'upstream failed' })
        if (ending === 'abort') {
            h.render().abortRequest()
            assert.equal(request.signal.aborted, true)
            // Clear synchronously, before the reader rejection/finally executes.
            assert.equal(h.render().activeToolCallMessage, null)
        }
        await completion
        await h.settle()
        assert.equal(h.render().activeToolCallMessage, null)
        assert.equal(h.render().sending, false)
        assert.deepEqual(h.errors, ending === 'error' ? ['upstream failed'] : [])
        assert(h.messages.includes(previous), 'completed tool message remains in history')

        const next = await h.start()
        assert.equal(h.requests.length, 2)
        assert.equal(h.render().activeToolCallMessage, null)
        await h.message(next.request, toolMessage('next'))
        assert.equal(h.render().activeToolCallMessage, h.messages.at(-1))
        assert.notEqual(h.render().activeToolCallMessage, previous)
        next.request.event('[DONE]')
        await next.completion
        await h.settle()
        assert.equal(h.render().activeToolCallMessage, null)
    })
}

test('switching sessions aborts the old request and clears active tools', async () => {
    const h = harness()
    const old = await h.start()
    await h.message(old.request, toolMessage('old-session'))
    assert.equal(h.render().activeToolCallMessage, h.messages.at(-1))
    h.switchSession()
    assert.equal(old.request.signal.aborted, true)
    assert.equal(h.render().activeToolCallMessage, null)
    await old.completion
    await h.settle()
    assert.equal(h.render().activeToolCallMessage, null)
    assert.deepEqual(h.errors, [])
    const next = await h.start()
    assert.equal(next.request.id, 'session-b')
    assert.equal(h.render().activeToolCallMessage, null)
    await h.message(next.request, toolMessage('new-session'))
    assert.equal(h.render().activeToolCallMessage, h.messages.at(-1))
    next.request.end()
    await next.completion
    await h.settle()
    assert.equal(h.render().activeToolCallMessage, null)
})
