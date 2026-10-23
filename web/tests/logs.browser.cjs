/*
 * Isolated log-viewer regression. Build web and Go first; install playwright outside the repo.
 * NODE_PATH=/tmp/cornerstone-log-check/node_modules CORNERSTONE_BINARY=/tmp/cornerstone-log-check/cornerstone \
 *   node web/tests/logs.browser.cjs
 * CHROME_PATH may override the locally installed Chrome executable.
 */
const assert = require('node:assert/strict')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')
const net = require('node:net')
const { spawn } = require('node:child_process')
const { setTimeout: delay } = require('node:timers/promises')
const { chromium } = require('playwright')

async function until(check) {
    for (let attempt = 0; attempt < 200; attempt++) {
        if (await check()) return
        await delay(25)
    }
    throw new Error('Timed out waiting for test condition')
}

const entry = (offset) => ({
    offset,
    time: '2026-10-23T08:00:00Z',
    level: 'WARN',
    message: `fixture ${offset}`,
    raw: `2026/10/23 08:00:00 x.go:1: [WARN] fixture ${offset}`,
})
const tail = (start = 1000, hasMore = true) => ({
    entries: Array.from({ length: 200 }, (_, index) => entry(start + index)),
    cursor: start + 200,
    next_before: start,
    has_more: hasMore,
    size: start + 200,
    file: 'cornerstone.log',
})
const fulfill = (route, data) => route.fulfill({ json: { success: true, data } })

;(async () => {
    assert(process.env.CORNERSTONE_BINARY, 'Use a test binary, not a running user instance')
    const artifacts = fs.mkdtempSync(path.join(os.tmpdir(), 'cornerstone-logs-'))
    const listener = net.createServer()
    await new Promise((resolve) => listener.listen(0, '127.0.0.1', resolve))
    const port = listener.address().port
    await new Promise((resolve) => listener.close(resolve))
    const base = `http://127.0.0.1:${port}`
    const log = fs.openSync(path.join(artifacts, 'server.log'), 'w')
    const server = spawn(
        process.env.CORNERSTONE_BINARY,
        ['-port', String(port), '-data', path.join(artifacts, 'data'), '-web', path.resolve(__dirname, '../dist')],
        { stdio: ['ignore', log, log] }
    )
    let browser
    try {
        await until(async () => {
            try {
                return (await fetch(base + '/management/health')).ok
            } catch {
                return false
            }
        })
        const response = await fetch(base + '/management/auth/setup', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username: 'logs-fixture', password: 'fixture-only-password-12345' }),
        })
        const auth = await response.json()
        assert(auth.success, JSON.stringify(auth))
        browser = await chromium.launch({
            executablePath: process.env.CHROME_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
            headless: true,
        })
        const context = await browser.newContext({
            viewport: { width: 1440, height: 1000 },
            reducedMotion: 'reduce',
            serviceWorkers: 'block',
        })
        await context.addInitScript((token) => {
            localStorage.setItem('cornerstone.auth.token', token)
            localStorage.setItem('cornerstone.locale', 'en')
            localStorage.setItem('cornerstone.logs.prefs', JSON.stringify({ level: 'all', hideHttp: true, follow: false }))
        }, auth.data.token)
        const page = await context.newPage()
        page.setDefaultTimeout(10000)
        const errors = []
        page.on('pageerror', (error) => errors.push(error.message))
        page.on('console', (message) => {
            if (message.type() === 'error') errors.push(message.text())
        })
        const older = []
        const polls = []
        let holdTail = false
        let heldTail
        await page.route('**/api/logs?*', async (route) => {
            const params = new URL(route.request().url()).searchParams
            if (params.has('before')) {
                older.push(route)
            } else if (params.has('after')) {
                polls.push(Number(params.get('after')))
                // Include an overlapping row to verify defensive merge deduplication.
                await fulfill(route, { ...tail(1001, false), entries: [entry(1200), entry(1201)], cursor: 1202 })
            } else if (holdTail) {
                heldTail = route
            } else {
                await fulfill(route, tail())
            }
        })
        await page.clock.install({ time: new Date('2026-10-23T08:00:00Z') })
        await page.goto(base + '/logs')
        await page.waitForFunction(() => document.querySelectorAll('[data-offset]').length === 200)

        // A stale older response must not leave the next generation's loading flag stuck.
        await page.locator('.logs-footer').scrollIntoViewIfNeeded()
        await until(() => older.length === 1)
        await Promise.all([
            page.waitForResponse((response) => new URL(response.url()).searchParams.get('level') === 'WARN,ERROR,FATAL'),
            page.getByRole('button', { name: 'Warnings+', exact: true }).click(),
        ])
        await page.waitForFunction(() => document.querySelector('.logs-scroll')?.getAttribute('aria-busy') === 'false')
        await fulfill(older[0], { ...tail(), entries: [entry(998)] })
        await page.locator('.logs-footer').scrollIntoViewIfNeeded()
        await until(() => older.length === 2).catch(async (error) => {
            throw new Error(`${error.message}: ${JSON.stringify(await page.locator('.logs-scroll').evaluate((element) => ({
                top: element.scrollTop, height: element.clientHeight, total: element.scrollHeight,
                busy: element.getAttribute('aria-busy'), footer: element.querySelector('.logs-footer')?.textContent,
            })))}, requests=${older.length}`)
        })
        await fulfill(older[1], { ...tail(), entries: [entry(999)], has_more: false, next_before: 0 })
        await page.locator('[data-offset="999"]').waitFor()
        assert.equal(await page.locator('[data-offset="998"]').count(), 0)
        console.log('PASS filter change releases stale history loading and ignores its response')

        // Polling and history requests must pause while a replacement tail is outstanding.
        await page.getByRole('button', { name: 'Live', exact: true }).click()
        holdTail = true
        await page.getByRole('button', { name: 'Refresh', exact: true }).click()
        await until(() => heldTail)
        await page.clock.runFor(4500)
        assert.deepEqual(polls, [], 'polling used an old cursor during refresh')
        assert.equal(older.length, 2, 'history loaded during refresh')
        await fulfill(heldTail, tail(1001, false))
        await page.waitForFunction(() => document.querySelector('.logs-scroll')?.getAttribute('aria-busy') === 'false')
        await page.clock.runFor(2000)
        await page.locator('[data-offset="1201"]').waitFor()
        assert.equal(polls[0], 1201, 'polling did not resume from the new tail cursor')
        assert.equal(await page.locator('[data-offset="1200"]').count(), 1)
        assert.equal(await page.locator('[data-offset]').count(), 201)
        console.log('PASS tail reload pauses polling; resumed polling uses the new cursor and deduplicates rows')

        // Simulate non-secure HTTP: Clipboard API is absent. The fallback copies the full raw line.
        await page.evaluate(() => {
            Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
            document.execCommand = (command) => {
                window.copiedText = command === 'copy' ? document.activeElement.value : undefined
                return command === 'copy'
            }
        })
        await page.locator('[data-offset="1201"] [role="button"]').click()
        await page.getByRole('button', { name: 'Copy raw line', exact: true }).click()
        assert.equal(await page.evaluate(() => window.copiedText), entry(1201).raw)
        assert.equal(await page.locator('textarea').count(), 0, 'fallback left a temporary textarea behind')
        await page.evaluate(() => {
            document.execCommand = () => { throw new Error('Copy denied') }
        })
        await page.getByRole('button', { name: 'Copy raw line', exact: true }).click()
        await page.getByText('Could not copy', { exact: true }).waitFor()
        assert.equal(await page.locator('textarea').count(), 0)
        console.log('PASS HTTP clipboard fallback and failure toast without an uncaught exception')
        assert.deepEqual(errors, [])
    } finally {
        if (browser) await browser.close()
        const exited = new Promise((resolve) => server.once('exit', resolve))
        if (server.exitCode === null) {
            server.kill('SIGTERM')
            await exited
        }
        fs.closeSync(log)
        fs.rmSync(artifacts, { recursive: true, force: true })
    }
})().catch((error) => {
    console.error(error)
    process.exitCode = 1
})
