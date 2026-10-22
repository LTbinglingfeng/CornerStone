import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { fileURLToPath } from 'node:url'
import { build } from 'vite'

// Compile the actual hook/components with the same TSX transformer as production.
async function load(relativePath, modules) {
    const entry = fileURLToPath(new URL(relativePath, import.meta.url))
    const compiled = await build({
        configFile: false,
        logLevel: 'silent',
        oxc: { jsx: { runtime: 'automatic' } },
        build: {
            write: false,
            minify: false,
            ssr: entry,
            rolldownOptions: {
                external: id => id !== entry,
                output: { format: 'cjs', exports: 'named' },
            },
        },
    })
    assert(!Array.isArray(compiled))
    const code = compiled.output.find(entry => entry.type === 'chunk' && entry.isEntry).code
    const module = { exports: {} }
    vm.runInNewContext(code, {
        module,
        exports: module.exports,
        require: id => { assert(id in modules, id); return modules[id] },
    })
    return module.exports
}

const { useDisplayItems } = await load('../src/components/ChatDetail/hooks/useDisplayItems.ts', {
    react: { useMemo: fn => fn() },
    '../utils': {
        getAssistantMessageSplitToken: () => '',
        isRecalledMessage: () => false,
        normalizeAssistantContent: value => value,
        splitAssistantMessageContent: value => value ? [value] : [],
    },
})
const jsx = (type, props) => ({ type, props })
const { ToolCallCard } = await load('../src/components/ChatDetail/components/ToolCallCard.tsx', {
    'react/jsx-runtime': { jsx, jsxs: jsx },
    '../../../contexts/I18nContext': { useT: () => ({ t: key => key }) },
    '../../../constants/toolControls': { TOOL_CONTROL_DEFINITIONS: [] },
    '../../ui': { Icon: 'Icon' },
})

let serial = 0
const call = (id = 'call_0_0', name = 'lookup') => ({
    id, type: 'function', function: { name, arguments: '{}' },
})
const assistant = (calls = [call()]) => ({
    role: 'assistant', content: '', timestamp: `message-${++serial}`, tool_calls: calls,
})
const user = () => ({ role: 'user', content: 'hello', timestamp: `message-${++serial}` })
const result = (data, id = 'call_0_0', tool = 'lookup', ok = true) => ({
    role: 'tool', tool_call_id: id, content: JSON.stringify({ ok, tool, data }),
    timestamp: `message-${++serial}`,
})
const display = (messages, activeToolCallMessage = null, sending = false) => useDisplayItems({
    messages, activeToolCallMessage, sending,
    streamingTimestamp: null, revealingTimestamp: null, visibleSegments: 0,
})
const cards = (...args) => display(...args).filter(item => item.type === 'tool-call')
const status = item => ToolCallCard(item).props.className

test('reused IDs retain their own results across turns and within one tool loop', () => {
    for (const boundary of [[], [user()]]) {
        const first = assistant(), second = assistant()
        const beijing = result('Beijing'), shanghai = result('Shanghai')
        const items = cards([first, beijing, ...boundary, second, shanghai])
        assert.equal(items.length, 2)
        assert.equal(items[0].result, beijing.content)
        assert.equal(items[1].result, shanghai.content)
    }
})

test('interleaved red packet receipts do not steal pending tool results', () => {
    const current = assistant([call('lookup'), call('other')])
    const receipt = assistant([call('red_packet_received_packet', 'red_packet_received')])
    const answer = result('Beijing', 'lookup'), other = result('Shanghai', 'other')
    const items = cards([current, receipt, other, answer], current, true)
    assert.equal(items.length, 2)
    assert.equal(items[0].result, answer.content)
    assert.equal(items[1].result, other.content)
    assert(items.every(item => status(item) === 'tool-call-card is-ok'))
})

test('a reused pending ID belongs to the nearest call, not an earlier unfinished step', () => {
    const previous = assistant(), current = assistant(), answer = result('current')
    const items = cards([previous, current, answer])
    assert.equal(items[0].result, undefined)
    assert.equal(items[1].result, answer.content)
})

test('a new call cannot borrow the completed result of an older call', () => {
    const old = assistant(), current = assistant()
    const items = cards([old, result('old'), user(), current], current, true)
    assert.equal(items[1].result, undefined)
    assert.equal(items[1].isRunning, true)
    assert.equal(status(items[1]), 'tool-call-card is-running')
})

test('old missing results stay missing while a different request is running', () => {
    const old = assistant(), current = assistant()
    const messages = [old, user(), current]
    let items = cards(messages, current, true)
    assert.equal(status(items[0]), 'tool-call-card is-missing')
    assert.equal(status(items[1]), 'tool-call-card is-running')
    // Clearing request state must stop the spinner even during the reply reveal animation.
    items = cards(messages, null, true)
    assert(items.every(item => status(item) === 'tool-call-card is-missing'))
    assert(cards(messages, current, false).every(item => !item.isRunning))
})

test('only the current tool step runs, not earlier unfinished steps', () => {
    const previousStep = assistant([call('first')]), current = assistant([call('second')])
    const items = cards([previousStep, current], current, true)
    assert.equal(items[0].isRunning, false)
    assert.equal(items[1].isRunning, true)
})

test('parallel results match by ID and completed calls stop running immediately', () => {
    const current = assistant([call('a'), call('b'), call('c')])
    const a = result('A', 'a'), b = result('B', 'b', 'lookup', false)
    const items = cards([current, b, a], current, true)
    assert.equal(items[0].result, a.content)
    assert.equal(status(items[0]), 'tool-call-card is-ok')
    assert.equal(items[1].result, b.content)
    assert.equal(status(items[1]), 'tool-call-card is-error')
    assert.equal(status(items[2]), 'tool-call-card is-running')
})

test('orphan results and non-tool message boundaries cannot complete another call', () => {
    for (const boundary of [user(), assistant([])]) {
        const current = assistant()
        const items = cards([result('orphan'), current, boundary, result('late')])
        assert.equal(items.length, 1)
        assert.equal(items[0].result, undefined)
    }
    const current = assistant()
    assert.equal(cards([current, result('unrelated', 'other')])[0].result, undefined)
})

test('no_reply and attachment success banners are scoped to their own tool step', () => {
    for (const [name, banner] of [
        ['no_reply', 'no-reply-banner'], ['send_attachment', 'attachment-intent-banner'],
    ]) {
        const first = assistant([call('same', name)]), second = assistant([call('same', name)])
        const items = display([first, result(null, 'same', name), user(), second,
            result(null, 'same', name, false)])
        const banners = items.filter(item => item.type === banner)
        assert.equal(banners.length, 1)
        assert.equal(banners[0].message, first)
        const failed = items.find(item => item.type === 'tool-call')
        assert.equal(failed.message, second)
        assert.equal(status(failed), 'tool-call-card is-error')
    }
})

test('empty and malformed results remain attached instead of looking pending', () => {
    for (const content of ['', 'not JSON', 'null']) {
        const current = assistant()
        const item = cards([current, { ...result(null), content }], current, true)[0]
        assert.equal(item.result, content)
        assert.equal(item.isRunning, false)
        assert.equal(status(item), 'tool-call-card is-ok')
    }
})
