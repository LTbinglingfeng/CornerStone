/*
 * Isolated QQ command-permission regression. No real credentials or user data.
 * Build web first, then run with a test binary and an external Playwright install:
 * NODE_PATH=/path/to/node_modules CORNERSTONE_BINARY=/tmp/cornerstone-test \
 *   node web/tests/qqofficial-commands.browser.cjs
 */
const assert = require('node:assert/strict')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')
const net = require('node:net')
const { spawn } = require('node:child_process')
const { chromium } = require('playwright')

;(async () => {
    assert(process.env.CORNERSTONE_BINARY, 'Use an isolated test binary')
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'cornerstone-qq-commands-'))
    const listener = net.createServer()
    await new Promise((resolve) => listener.listen(0, '127.0.0.1', resolve))
    const port = listener.address().port
    await new Promise((resolve) => listener.close(resolve))
    const base = `http://127.0.0.1:${port}`
    const log = fs.openSync(path.join(directory, 'server.log'), 'w')
    const server = spawn(
        process.env.CORNERSTONE_BINARY,
        ['-port', String(port), '-data', path.join(directory, 'data'), '-web', path.resolve(__dirname, '../dist')],
        { stdio: ['ignore', log, log] }
    )
    let browser
    let token
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
    try {
        for (let attempt = 0; ; attempt++) {
            try {
                await fetch(base + '/management/health')
                break
            } catch (error) {
                if (attempt >= 100) throw error
                await new Promise((resolve) => setTimeout(resolve, 100))
            }
        }
        token = (await api('/management/auth/setup', 'POST', {
            username: 'qq-command-fixture',
            password: 'fixture-only-password-12345',
        })).token
        browser = await chromium.launch({
            executablePath: process.env.CHROME_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
            headless: true,
        })
        for (const [locale, theme, width] of [['en', 'light', 1440], ['zh', 'dark', 390]]) {
            const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: 'reduce', serviceWorkers: 'block' })
            await context.addInitScript(({ token, locale, theme }) => {
                localStorage.setItem('cornerstone.auth.token', token)
                localStorage.setItem('cornerstone.locale', locale)
                localStorage.setItem('theme', theme)
            }, { token, locale, theme })
            const page = await context.newPage()
            const errors = []
            page.on('pageerror', (error) => errors.push(error.message))
            // Fail the test if saving permissions accidentally initiates a real login.
            await page.route('**/api/channel/qq-official/login/**', (route) => {
                errors.push('unexpected QR request')
                return route.abort()
            })
            await page.goto(base + '/channels?channel=qq-official')
            const list = page.getByRole('switch', { name: '/ls', exact: true })
            await list.waitFor()
            for (const command of ['new', 'ls', 'checkout', 're', 'rename', 'delete', 'prompt']) {
                assert.equal(await page.getByRole('switch', { name: `/${command}`, exact: true }).count(), 1)
            }
            assert.equal(await page.getByRole('switch', { name: '/menu', exact: true }).count(), 0)
            const next = !(await list.isChecked())
            await list.setChecked(next, { force: true })
            const save = page.locator('.save-button')
            await save.click()
            await page.waitForFunction(() => !document.querySelector('.save-button').disabled)
            assert.equal((await api('/api/settings/qq-official')).command_permissions.ls, next)
            await page.reload()
            await list.waitFor()
            assert.equal(await list.isChecked(), next)
            assert.equal(await page.getByRole('switch', { name: '/new', exact: true }).isChecked(), true)
            assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true)
            assert.deepEqual(errors, [])
            console.log(`PASS QQ command permissions: ${locale}/${theme}/${width}px save and reload`)
            await context.close()
        }
    } finally {
        if (browser) await browser.close()
        server.kill('SIGTERM')
        fs.closeSync(log)
        console.log(`Isolated artifacts: ${directory}`)
    }
})().catch((error) => {
    console.error(error)
    process.exitCode = 1
})
