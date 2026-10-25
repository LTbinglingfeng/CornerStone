import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { fileURLToPath } from 'node:url'
import { build } from 'vite'

const hookPath = fileURLToPath(new URL('../src/components/ChatDetail/hooks/useDisplayItems.ts', import.meta.url))
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
const modules = {
    react: { useMemo: fn => fn() },
    '../utils': {
        getAssistantMessageSplitToken: () => '→',
        isRecalledMessage: () => false,
        normalizeAssistantContent: value => value,
        splitAssistantMessageContent: value => value ? value.split('→') : [],
    },
}
const module = { exports: {} }
vm.runInNewContext(code, {
    module, exports: module.exports,
    require: id => { assert(id in modules, id); return modules[id] },
})
const message = (role, content, second) => ({ role, content, timestamp: `2026-01-01T00:00:0${second}Z` })
const display = (messages, transientErrors) => module.exports.useDisplayItems({
    messages, transientErrors, sending: false, activeToolCallMessage: null,
    streamingTimestamp: null, revealingTimestamp: null, visibleSegments: 0,
})

test('temporary errors survive refreshed history without changing persisted indices', () => {
    const first = message('user', 'hello', 1)
    const error = message('assistant', 'Provider failed → retry later', 2)
    const next = message('user', 'retry', 3)
    const errors = [error]
    const items = display([first, next], errors)
    assert.equal(items.length, 3)
    assert.equal(items[1].type, 'error')
    assert.equal(items[1].message.content, error.content, 'error is not parsed or split as an AI reply')
    assert.equal(items[1].messageIndex, -1)
    assert.equal(items[2].messageIndex, 1, 'error does not shift edit/delete indices')
    const refreshed = display([{ ...first }, { ...next }], errors)
    assert.equal(refreshed[1].key, items[1].key)
    assert.equal(refreshed[1].type, 'error')
    assert.equal(display([first, next], []).length, 2)
})
