import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { fileURLToPath } from 'node:url'
import { build } from 'vite'

// TypeScript 7 no longer exposes transpileModule; use the production TSX transformer.
const componentPath = fileURLToPath(new URL('../src/components/QQOfficialSettings.tsx', import.meta.url))
const compiled = await build({
    configFile: false,
    logLevel: 'silent',
    oxc: { jsx: { runtime: 'automatic' } },
    build: {
        write: false,
        minify: false,
        ssr: componentPath,
        rolldownOptions: {
            external: id => id !== componentPath,
            output: { format: 'cjs', exports: 'named' },
        },
    },
})
assert(!Array.isArray(compiled))
const code = compiled.output.find(entry => entry.type === 'chunk' && entry.isEntry).code

// Execute the actual component with a small hook/JSX harness; no DOM or real QQ traffic.
function harness() {
    const initial = { revision: '1', enabled: false, app_id: '123', has_secret: true,
        prompt_id: '', source_filter_mode: 'all', allowed_user_openids: [], status: 'disabled' }
    const slots = [], effects = [], timers = new Map(), calls = [], toasts = []
    let cursor = 0, timerID = 0, current = initial, cancelResult = { status: 'cancelled' }
    const service = {
        async getSettings() { return current },
        async start() { calls.push('start'); return { id: 'login', qr_url: 'fake', expires_at: new Date(Date.now() + 60000).toISOString() } },
        async cancel() { calls.push('cancel'); return cancelResult },
        async updateSettings(form) { calls.push(['save', form]); current = { ...current, ...form, revision: String(Number(current.revision) + 1) }; return current },
    }
    const react = {
        useState(value) { const i = cursor++; if (!(i in slots)) slots[i] = value; return [slots[i], next => { slots[i] = typeof next === 'function' ? next(slots[i]) : next }] },
        useRef(value) { const i = cursor++; return slots[i] ??= { current: value } },
        useEffect(fn) { cursor++; if (!effects.length) effects.push(fn) },
    }
    const jsx = (type, props) => ({ type, props })
    const modules = {
        react, 'react/jsx-runtime': { jsx, jsxs: jsx }, qrcode: { async toDataURL() { return 'fake-image' } },
        '../services/api': { async getPrompts() { return [] } },
        '../services/qqOfficialService': { qqOfficialService: service },
        '../contexts/I18nContext': { useT: () => ({ t: key => key, locale: 'en' }) },
        '../contexts/ToastContext': { useToast: () => ({ showToast: (...args) => toasts.push(args) }) },
        '../i18n/channels': { getChannelsCopy: () => ({ detail: {} }) },
        './ui': Object.fromEntries(['Badge', 'Group', 'Row', 'Toggle', 'Icon'].map(k => [k, k])),
        './provider': { CustomSelect: 'CustomSelect' }, './channelStatus': { channelStatusTone() {} }, './ClawBotSettings.css': {},
    }
    const module = { exports: {} }
    vm.runInNewContext(code, { module, exports: module.exports, require: id => { assert(id in modules, id); return modules[id] }, AbortController,
        setTimeout: (fn, delay) => { timers.set(++timerID, { fn, delay }); return timerID }, clearTimeout: id => timers.delete(id), console })
    function render() { cursor = 0; return module.exports.default({ onBack() {} }) }
    function nodes(node, result = []) {
        if (!node || typeof node !== 'object') return result
        if (Array.isArray(node)) { node.forEach(n => nodes(n, result)); return result }
        result.push(node)
        Object.values(node.props || {}).forEach(value => nodes(value, result))
        return result
    }
    const find = predicate => nodes(render()).find(predicate)
    const button = text => find(n => n.type === 'button' && n.props.children === text)
    const flush = async () => { for (let i = 0; i < 25; i++) await Promise.resolve() }
    render(); effects[0]()
    return { calls, toasts, button, find, flush, timers, setCreated() { current = { ...initial, revision: '2', app_id: '456' }; cancelResult = { status: 'created' } } }
}

test('QR in flight disables saving and guards the handler', async () => {
    const h = harness(); await h.flush()
    const staleSave = h.button('napCat.saveSettings')
    h.button('qqOfficial.start').props.onClick(); await h.flush()
    assert.equal(h.button('napCat.saveSettings').props.disabled, true)
    h.button('napCat.saveSettings').props.onClick(); await h.flush()
    assert.equal(h.calls.some(c => Array.isArray(c)), false)
    assert(staleSave)
})

test('cancel after server completion synchronizes the new account before saving', async () => {
    const h = harness(); await h.flush()
    h.button('qqOfficial.start').props.onClick(); await h.flush()
    h.setCreated()
    h.button('qqOfficial.cancel').props.onClick()
    assert.equal(h.button('napCat.saveSettings').props.disabled, true)
    await h.flush()
    assert.equal(h.find(n => n.props?.id === 'qq-official-app').props.value, '456')
    h.button('napCat.saveSettings').props.onClick(); await h.flush()
    const saved = h.calls.find(c => Array.isArray(c))[1]
    assert.equal(saved.app_id, '456')
    assert.equal(saved.revision, '2')
})

for (const [name, edit] of [
    ['persona', h => h.find(n => n.type === 'CustomSelect' && n.props.ariaLabel === 'napCat.bindPersona').props.onChange('new-persona')],
    ['access mode', h => h.find(n => n.type === 'CustomSelect' && n.props.ariaLabel === 'napCat.sourceFilterMode').props.onChange('allowlist')],
    ['allowlist', h => h.find(n => n.props?.id === 'qq-official-users').props.onChange({ target: { value: 'new-openid' } })],
    ['AppID', h => h.find(n => n.props?.id === 'qq-official-app').props.onChange({ target: { value: '456' } })],
    ['secret', h => h.find(n => n.props?.id === 'qq-official-secret').props.onChange({ target: { value: 'new-secret' } })],
    ['clear secret', h => h.find(n => n.type === 'Toggle' && n.props.label === 'qqOfficial.clear').props.onChange(true)],
    ['enabled', h => h.find(n => n.type === 'Toggle' && n.props.label === 'qqOfficial.enable').props.onChange(true)],
]) {
    test(`unsaved ${name} blocks QR creation with an explicit save-first message`, async () => {
        const h = harness(); await h.flush()
        edit(h)
        const start = h.button('qqOfficial.start')
        assert.equal(start.props.disabled, true)
        assert(h.find(n => n.type === 'Group' && n.props.footer === 'qqOfficial.saveBeforeScan'))
        start.props.onClick(); await h.flush()
        assert.deepEqual(h.calls, [])
        assert.equal(h.toasts[0][0], 'qqOfficial.saveBeforeScan')
    })
}

test('saving edits restores the clean-form QR flow', async () => {
    const h = harness(); await h.flush()
    h.find(n => n.props?.id === 'qq-official-users').props.onChange({ target: { value: 'new-openid' } })
    h.button('napCat.saveSettings').props.onClick(); await h.flush()
    assert.equal(h.button('qqOfficial.start').props.disabled, false)
    assert.equal(h.find(n => n.props?.id === 'qq-official-users').props.value, 'new-openid')
    h.button('qqOfficial.start').props.onClick(); await h.flush()
    assert.equal(h.calls.filter(c => c === 'start').length, 1)
    assert.equal(h.button('napCat.saveSettings').props.disabled, true)
})
