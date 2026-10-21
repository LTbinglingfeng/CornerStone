import assert from 'node:assert/strict'
import test from 'node:test'
import fs from 'node:fs'
import vm from 'node:vm'
import ts from 'typescript'

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
        async updateSettings(form) { calls.push(['save', form]); return current },
    }
    const react = {
        useState(value) { const i = cursor++; if (!(i in slots)) slots[i] = value; return [slots[i], next => { slots[i] = typeof next === 'function' ? next(slots[i]) : next }] },
        useRef(value) { const i = cursor++; return slots[i] ??= { current: value } },
        useEffect(fn) { cursor++; if (!effects.length) effects.push(fn) },
    }
    const jsx = (type, props) => ({ type, props })
    const modules = {
        react, 'react/jsx-runtime': { jsx, jsxs: jsx }, qrcode: { default: { async toDataURL() { return 'fake-image' } } },
        '../services/api': { async getPrompts() { return [] } },
        '../services/qqOfficialService': { qqOfficialService: service },
        '../contexts/I18nContext': { useT: () => ({ t: key => key, locale: 'en' }) },
        '../contexts/ToastContext': { useToast: () => ({ showToast: (...args) => toasts.push(args) }) },
        '../i18n/channels': { getChannelsCopy: () => ({ detail: {} }) },
        './ui': Object.fromEntries(['Badge', 'Group', 'Row', 'Toggle', 'Icon'].map(k => [k, k])),
        './provider': { CustomSelect: 'CustomSelect' }, './channelStatus': { channelStatusTone() {} }, './ClawBotSettings.css': {},
    }
    const source = fs.readFileSync(new URL('../src/components/QQOfficialSettings.tsx', import.meta.url), 'utf8')
    const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX } }).outputText
    const exports = {}
    vm.runInNewContext(code, { exports, require: id => { assert(id in modules, id); return modules[id] }, AbortController,
        setTimeout: (fn, delay) => { timers.set(++timerID, { fn, delay }); return timerID }, clearTimeout: id => timers.delete(id), console })
    function render() { cursor = 0; return exports.default({ onBack() {} }) }
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
