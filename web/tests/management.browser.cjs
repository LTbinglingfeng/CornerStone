/*
 * Isolated browser regression: never reads or writes the application's real data.
 * Build web and Go first. Install playwright and @axe-core/playwright outside the repo.
 * NODE_PATH=/tmp/cornerstone-ui-check/node_modules CORNERSTONE_BINARY=/tmp/cornerstone-ui-check/cornerstone \
 *   node web/tests/management.browser.cjs
 * CHROME_PATH may override the locally installed Chrome executable.
 */
const assert = require('node:assert/strict')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')
const http = require('node:http')
const net = require('node:net')
const { spawn } = require('node:child_process')
const { chromium } = require('playwright')
const { default: AxeBuilder } = require('@axe-core/playwright')

async function freePort() {
    const server = net.createServer()
    await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve))
    const port = server.address().port
    await new Promise((resolve) => server.close(resolve))
    return port
}

;(async () => {
    assert(process.env.CORNERSTONE_BINARY, 'Use a test binary, not a running user instance')
    const artifacts = fs.mkdtempSync(path.join(os.tmpdir(), 'cornerstone-management-'))
    const staticDir = path.join(artifacts, 'web')
    fs.cpSync(path.resolve(__dirname, '../dist'), staticDir, { recursive: true })
    const port = await freePort()
    const base = `http://127.0.0.1:${port}`
    const log = fs.openSync(path.join(artifacts, 'server.log'), 'w')
    const server = spawn(
        process.env.CORNERSTONE_BINARY,
        ['-port', String(port), '-data', path.join(artifacts, 'data'), '-web', staticDir],
        { stdio: ['ignore', log, log] }
    )
    const upstream = http.createServer(async (req, res) => {
        let body = ''
        for await (const chunk of req) body += chunk
        const request = JSON.parse(body || '{}')
        const text = 'Local fixture reply verified.'
        if (request.stream) {
            res.writeHead(200, { 'Content-Type': 'text/event-stream' })
            res.write(
                `data: ${JSON.stringify({ id: 'fixture', object: 'chat.completion.chunk', choices: [{ index: 0, delta: { content: text }, finish_reason: null }] })}\n\n`
            )
            res.end('data: [DONE]\n\n')
        } else {
            res.writeHead(200, { 'Content-Type': 'application/json' })
            res.end(
                JSON.stringify({
                    id: 'fixture',
                    choices: [{ index: 0, message: { role: 'assistant', content: text }, finish_reason: 'stop' }],
                })
            )
        }
    })
    await new Promise((resolve) => upstream.listen(0, '127.0.0.1', resolve))
    let browser
    let page
    let token
    const errors = []
    const accessibility = []
    const api = async (url, method = 'GET', body) => {
        const response = await fetch(base + url, {
            method,
            headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
            ...(body ? { body: JSON.stringify(body) } : {}),
        })
        const result = await response.json()
        assert(response.ok && result.success, `${method} ${url}: ${JSON.stringify(result)}`)
        return result.data
    }
    const check = async (name, task) => {
        await task()
        console.log(`PASS ${name}`)
    }
    try {
        for (let attempt = 0; ; attempt++) {
            try {
                await fetch(base + '/management/health')
                break
            } catch (error) {
                if (attempt > 100) throw error
                await new Promise((resolve) => setTimeout(resolve, 100))
            }
        }
        const auth = await api('/management/auth/setup', 'POST', {
            username: 'ui-fixture',
            password: 'fixture-only-password-12345',
        })
        token = auth.token
        browser = await chromium.launch({
            executablePath: process.env.CHROME_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
            headless: true,
        })
        const context = await browser.newContext({
            viewport: { width: 1440, height: 1000 },
            reducedMotion: 'reduce',
            serviceWorkers: 'block',
        })
        await context.addInitScript((value) => {
            localStorage.setItem('cornerstone.auth.token', value)
            if (!localStorage.getItem('cornerstone.locale')) localStorage.setItem('cornerstone.locale', 'en')
            if (!localStorage.getItem('theme')) localStorage.setItem('theme', 'light')
        }, token)
        page = await context.newPage()
        page.on('pageerror', (error) => errors.push(error.message))
        const goto = async (route) => {
            await page.goto(base + route)
            await page.locator('.console-workspace').waitFor()
        }
        const audit = async (name) => {
            await page.waitForFunction(() =>
                Array.from(document.querySelectorAll('dialog[open], dialog[open] > *')).every(
                    (element) => Number.parseFloat(getComputedStyle(element).opacity) >= 0.99
                )
            )
            const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()
            accessibility.push({
                name,
                violations: results.violations.map((v) => ({
                    id: v.id,
                    impact: v.impact,
                    nodes: v.nodes.map((n) => ({ target: n.target, summary: n.failureSummary })),
                })),
            })
        }
        await check('default management landing and honest empty state', async () => {
            await goto('/')
            await page.getByRole('heading', { name: 'Overview', exact: true }).waitFor()
            await page.getByText('No conversations yet.', { exact: true }).waitFor()
            assert.equal(new URL(page.url()).pathname, '/overview')
            await audit('overview-light')
            await page.screenshot({ path: path.join(artifacts, 'overview-empty.png') })
        })
        const persona = await api('/management/prompts', 'POST', {
            name: 'Atlas',
            description: 'Local fixture persona',
            content: 'Reply concisely.',
        })
        await api('/management/prompts', 'POST', {
            name: 'Birch',
            description: 'Second fixture',
            content: 'Be helpful.',
        })
        const sessions = []
        for (let i = 0; i < 24; i++)
            sessions.push(
                await api('/management/sessions', 'POST', {
                    title: `Archive fixture ${String(i).padStart(2, '0')}`,
                    prompt_id: persona.id,
                })
            )
        const provider = await api('/management/providers', 'POST', {
            id: 'ui-fixture',
            name: 'Local fixture',
            type: 'openai',
            base_url: `http://127.0.0.1:${upstream.address().port}/v1`,
            api_key: 'fixture-only',
            model: 'local-test-model',
            stream: true,
            context_messages: 10,
            temperature: 0.7,
            top_p: 1,
        })
        await api('/management/providers/active', 'PUT', { provider_id: provider.id })
        await api('/management/config', 'PUT', { reply_wait_window_seconds: 0, assistant_message_split_token: '' })
        await check('all sessions, title search, persona filter and safe delete', async () => {
            await goto('/chat')
            await page.locator('.archive-card').nth(23).waitFor()
            assert.equal(await page.locator('.archive-card').count(), 24)
            await audit('archive')
            await page.locator('.search-input').fill('fixture 00')
            await page.waitForFunction(() => document.querySelectorAll('.archive-card').length === 1)
            await page.locator('.archive-delete').click()
            await page.locator('dialog[open]').waitFor()
            assert(await page.locator('dialog').evaluate((el) => el.matches(':modal')))
            await page.keyboard.press('Escape')
            await page.waitForFunction(() => !document.querySelector('dialog[open]'))
            assert.equal((await api('/management/sessions')).length, 24)
            await page.locator('.archive-delete').click()
            await page.locator('.confirm-modal-btn.confirm').click()
            await page.waitForFunction(() => document.querySelectorAll('.archive-card').length === 0)
            assert.equal((await api('/management/sessions')).length, 23)
            await page.locator('.search-input').fill('')
            await page.locator('.archive-filter select').selectOption({ label: 'Birch' })
            assert.equal(await page.locator('.archive-card').count(), 0)
            await page.locator('.archive-filter select').selectOption('')
        })
        await check('archive browsing position survives browser Back', async () => {
            const open = page.locator('.archive-open').last()
            await open.scrollIntoViewIfNeeded()
            const scrollTop = await page.locator('.archive').evaluate((el) => el.scrollTop)
            assert(scrollTop > 0)
            await open.click()
            await page.locator('.chat-detail').waitFor()
            await page.goBack()
            await page.waitForFunction(() => !document.querySelector('.chat-detail'))
            assert.equal(await page.locator('.archive').evaluate((el) => el.scrollTop), scrollTop)
        })
        await check('conversation deep link, web reply and history reload', async () => {
            await goto(`/chat/${sessions[1].id}?promptId=${persona.id}`)
            await page.locator('.chat-input').fill('Test web reply')
            await page.locator('.send-button').click()
            await page.getByText('Local fixture reply verified.', { exact: true }).waitFor({ timeout: 30000 })
            await page.reload()
            await page.getByText('Local fixture reply verified.', { exact: true }).waitFor()
            const record = await api(`/management/sessions/${sessions[1].id}`)
            assert(record.messages.some((message) => message.content === 'Local fixture reply verified.'))
            await page.screenshot({ path: path.join(artifacts, 'conversation.png') })
        })
        await check('streaming protocol remains readable through archive history', async () => {
            const response = await fetch(base + '/api/chat', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
                body: JSON.stringify({
                    session_id: sessions[2].id,
                    prompt_id: persona.id,
                    messages: [{ role: 'user', content: 'Test stream protocol' }],
                    stream: true,
                    save_history: true,
                }),
            })
            assert(response.ok)
            assert(response.headers.get('Content-Type').includes('text/event-stream'))
            const stream = await response.text()
            assert(stream.includes('[DONE]'))
            await goto(`/chat/${sessions[2].id}?promptId=${persona.id}`)
            await page.getByText('Local fixture reply verified.', { exact: true }).waitFor({ timeout: 30000 })
        })
        await check('legacy notification query opens intended conversation', async () => {
            await goto(`/?sessionId=${sessions[2].id}&promptId=${persona.id}`)
            await page.locator('.chat-detail').waitFor()
            assert.equal(new URL(page.url()).pathname, `/chat/${sessions[2].id}`)
        })
        await check('persona editing, native memory modal, keyboard selection and persistence', async () => {
            await goto('/contacts')
            await page.locator('.contact-card').nth(1).waitFor()
            await audit('personas')
            await page.screenshot({ path: path.join(artifacts, 'personas-desktop.png') })
            await page
                .locator('.contact-card')
                .filter({ hasText: 'Atlas' })
                .getByRole('button', { name: /Edit/ })
                .click()
            await page.locator('.name-input').fill('Atlas updated')
            await page.locator('.description-input').fill('Updated local fixture')
            await audit('persona-editor')
            await page.getByRole('button', { name: 'Memory Management', exact: true }).click()
            await page.locator('.memory-header-actions .add-btn').click()
            const dialog = page.locator('dialog[open]')
            await dialog.getByLabel('Content', { exact: true }).fill('Prefers clear management interfaces.')
            await dialog.getByRole('button', { name: 'Memory Type', exact: true }).click()
            await page.keyboard.press('ArrowDown')
            await page.keyboard.press('Escape')
            assert.equal(await page.locator('dialog[open]').count(), 1)
            await audit('memory-modal')
            await dialog.getByRole('button', { name: 'Add', exact: true }).click()
            await page.waitForFunction(() => !document.querySelector('dialog[open]'))
            assert(
                (await api(`/api/memory/${persona.id}`)).some(
                    (memory) => memory.content === 'Prefers clear management interfaces.'
                )
            )
            await page.locator('.persona-editor-header .save-btn').click()
            await page.waitForFunction(() => !document.querySelector('.persona-editor'))
            await page.getByRole('heading', { name: 'Atlas updated', exact: true }).waitFor()
            assert.equal((await api(`/management/prompts/${persona.id}`)).description, 'Updated local fixture')
            await page.reload()
            await page.getByRole('heading', { name: 'Atlas updated', exact: true }).waitFor()
        })
        await check('channel deep links, keyboard focus and saved permission', async () => {
            await goto('/channels')
            await page.locator('.channel-card').nth(1).waitFor()
            await audit('channels')
            await page.locator('.channel-card').first().getByRole('link').click()
            await page.getByLabel('Enable Channel', { exact: true }).waitFor({ state: 'attached' })
            await page.getByLabel('/new', { exact: true }).locator('..').click()
            await page.getByRole('button', { name: 'Save Settings', exact: true }).click()
            await page.waitForFunction(() => !document.querySelector('.clawbot-actions .save-button').disabled)
            assert.equal((await api('/api/settings/clawbot')).command_permissions.new, false)
            await audit('wechat')
            await page.getByRole('button', { name: 'Back', exact: true }).click()
            await page.locator('.channel-card').first().waitFor()
            await goto('/channels?channel=qq')
            await page.locator('.provider-settings-content').waitFor()
            await audit('qq')
        })
        await check('settings categories and provider native modal', async () => {
            for (const section of ['models', 'automation', 'memory', 'general']) {
                await goto(`/settings?section=${section}`)
                await page.locator('.settings-content').waitFor()
                await audit(`settings-${section}`)
                await page.screenshot({ path: path.join(artifacts, `settings-${section}.png`) })
            }
            await goto('/settings?section=models&panel=providers')
            await page.locator('.provider-card').first().waitFor()
            await page.locator('.provider-card').first().locator('.card-action-btn.edit').click()
            await page.locator('dialog[open]').waitFor()
            assert(await page.locator('dialog[open]').evaluate((el) => el.matches(':modal')))
            await audit('provider-modal')
            for (let i = 0; i < 20; i++) {
                await page.keyboard.press('Tab')
                assert(await page.evaluate(() => !!document.activeElement.closest('dialog[open]')))
            }
            await page.keyboard.press('Escape')
            await page.waitForFunction(() => !document.querySelector('dialog[open]'))
        })
        await check('general setting save and bilingual navigation', async () => {
            await goto('/settings?section=general')
            await page.getByRole('button', { name: /Time Zone/ }).click()
            await page.locator('dialog[open]').getByRole('textbox').fill('UTC')
            await page.locator('dialog[open]').getByRole('button', { name: 'Save', exact: true }).click()
            await page.waitForFunction(() => !document.querySelector('dialog[open]'))
            assert.equal((await api('/management/config')).time_zone, 'UTC')
            await page.getByRole('button', { name: /Language.*English/ }).click()
            await page.locator('dialog[open]').getByRole('button', { name: '中文', exact: true }).click()
            await page.getByRole('navigation', { name: '管理控制台' }).waitFor()
            assert.equal(await page.locator('html').getAttribute('lang'), 'zh-CN')
            await page.reload()
            await page.getByRole('navigation', { name: '管理控制台' }).waitFor()
            await page.getByRole('button', { name: /语言.*中文/ }).click()
            await page.locator('dialog[open]').getByRole('button', { name: 'English', exact: true }).click()
            await page.getByRole('navigation', { name: 'Management console' }).waitFor()
        })
        await check('mobile, dark theme, reduced motion and short viewport navigation', async () => {
            await page.setViewportSize({ width: 390, height: 844 })
            for (const route of ['/overview', '/channels', '/contacts', '/chat', '/settings?section=general', '/me']) {
                await goto(route)
                await page.waitForTimeout(250)
                assert(
                    await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
                    `horizontal overflow: ${route}`
                )
                await page.screenshot({ path: path.join(artifacts, `mobile-${route.split('?')[0].slice(1)}.png`) })
            }
            await page.getByRole('button', { name: 'Toggle color theme' }).click()
            await page.getByRole('link', { name: 'Overview', exact: true }).click()
            assert.equal(await page.locator('html').getAttribute('data-theme'), 'dark')
            await audit('overview-dark-mobile')
            await page.screenshot({ path: path.join(artifacts, 'overview-dark-mobile.png') })
            await page.setViewportSize({ width: 1100, height: 350 })
            await page.locator('.console-theme').scrollIntoViewIfNeeded()
            assert(await page.locator('.console-theme').isVisible())
        })
        await check('partial failures remain distinguishable from empty data', async () => {
            await page.route('**/api/settings/clawbot', (route) =>
                route.fulfill({
                    status: 503,
                    contentType: 'application/json',
                    body: JSON.stringify({ success: false, error: 'Fixture unavailable' }),
                })
            )
            await goto('/overview')
            await page.getByText('Some information could not be loaded. Other sections are still available.').waitFor()
            await page.getByText('local-test-model', { exact: true }).waitFor()
            await page.unroute('**/api/settings/clawbot')
            await page.getByRole('button', { name: 'Retry', exact: true }).click()
            await page.getByRole('button', { name: 'Refresh', exact: true }).waitFor()
        })
        await check('settings failures never expose editable fallback defaults', async () => {
            await page.route('**/management/providers', (route) =>
                route.fulfill({
                    status: 503,
                    contentType: 'application/json',
                    body: JSON.stringify({ success: false, error: 'Fixture unavailable' }),
                })
            )
            await goto('/settings?section=general')
            await page.locator('.settings-loading[role="alert"]').waitFor()
            assert.equal(await page.locator('.settings-entry-btn').count(), 0)
            await page.unroute('**/management/providers')
            await page.getByRole('button', { name: 'Refresh status', exact: true }).click()
            await page.locator('.settings-content').waitFor()
        })
        await check('channel deadlines allow retry without blocking independently loaded status', async () => {
            await page.clock.install()
            await page.route('**/api/settings/clawbot', () => {})
            await goto('/channels')
            await page.locator('.channel-card').nth(1).getByRole('status').filter({ hasText: 'Off' }).waitFor()
            await page.clock.fastForward(21000)
            await page.locator('.channel-card').first().getByText('Unavailable', { exact: true }).first().waitFor()
            assert(await page.getByRole('button', { name: 'Refresh status', exact: true }).isEnabled())
            await page.unroute('**/api/settings/clawbot')
            await page.getByRole('button', { name: 'Refresh status', exact: true }).click()
            await page.locator('.channel-card').first().getByRole('status').filter({ hasText: 'Off' }).waitFor()
            await page.clock.resume()
            await goto('/overview')
            await page.getByRole('button', { name: 'Refresh', exact: true }).waitFor()
        })
        await check('expired authentication returns to login without exposing private UI', async () => {
            await page.route('**/management/sessions', (route) =>
                route.fulfill({
                    status: 401,
                    contentType: 'application/json',
                    body: JSON.stringify({ success: false, error: 'Unauthorized' }),
                })
            )
            await page.getByRole('button', { name: 'Refresh', exact: true }).click()
            await page.locator('.auth-page').waitFor()
            assert.equal(await page.locator('.console-layout').count(), 0)
            assert.equal(await page.evaluate(() => localStorage.getItem('cornerstone.auth.token')), null)
            await page.unroute('**/management/sessions')
            await page.locator('#auth-login-password').fill('fixture-only-password-12345')
            await page.locator('.auth-button').click()
            await page.locator('.console-layout').waitFor()
            assert.equal(new URL(page.url()).pathname, '/overview')
        })
        assert.deepEqual(errors, [], 'Browser runtime errors')
        fs.writeFileSync(path.join(artifacts, 'accessibility.json'), JSON.stringify(accessibility, null, 2))
        console.log(
            'Accessibility findings:',
            accessibility
                .filter((a) => a.violations.length)
                .map((a) => `${a.name}: ${a.violations.map((v) => v.id).join(', ')}`)
                .join('\n') || 'none'
        )
        assert.equal(accessibility.filter((a) => a.violations.length > 0).length, 0, 'Accessibility regression')
        console.log(`Artifacts: ${artifacts}`)
    } catch (error) {
        if (page) {
            await page.screenshot({ path: path.join(artifacts, 'failure.png') }).catch(() => {})
            fs.writeFileSync(
                path.join(artifacts, 'failure.txt'),
                await page
                    .locator('body')
                    .innerText()
                    .catch(() => '')
            )
        }
        fs.writeFileSync(path.join(artifacts, 'accessibility.json'), JSON.stringify(accessibility, null, 2))
        console.error(`Artifacts: ${artifacts}`)
        throw error
    } finally {
        await browser?.close()
        server.kill('SIGTERM')
        upstream.close()
        fs.closeSync(log)
    }
})().catch((error) => {
    console.error(error)
    process.exitCode = 1
})
