import assert from 'node:assert/strict'
import test from 'node:test'
import { buildChatRoute, getRouteState, normalizePathname, tabOrder, tabRoutes } from '../src/utils/routes.ts'

test('management is the default entry and every navigation destination resolves', () => {
    for (const path of ['/', '/management', '/overview']) {
        assert.deepEqual(getRouteState(path), { activeTab: 'overview', activeSessionId: null })
    }
    for (const tab of tabOrder) {
        assert.equal(getRouteState(tabRoutes[tab]).activeTab, tab)
    }
})

test('legacy destinations and encoded conversation links remain supported', () => {
    assert.equal(getRouteState('/contacts').activeTab, 'contacts')
    assert.equal(getRouteState('/me').activeTab, 'me')
    assert.equal(getRouteState('/chat').activeTab, 'chat')
    const route = buildChatRoute('session / 中文', 'persona & test')
    const url = new URL(route, 'http://localhost')
    assert.equal(getRouteState(url.pathname).activeSessionId, 'session / 中文')
    assert.equal(url.searchParams.get('promptId'), 'persona & test')
})

test('normalization and invalid routes are safe', () => {
    assert.equal(normalizePathname('/settings///'), '/settings')
    assert.equal(getRouteState('/chat/id/extra'), null)
    assert.equal(getRouteState('/unknown'), null)
    assert.equal(getRouteState('/chat/%bad').activeSessionId, '%bad')
})
