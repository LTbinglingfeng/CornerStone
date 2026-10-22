import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { build } from 'vite'

test('production build keeps scripts, styles and logos inline and preserves the service worker', async () => {
    const root = fileURLToPath(new URL('../', import.meta.url))
    const result = await build({ root, logLevel: 'silent', build: { write: false } })
    assert(!Array.isArray(result))
    const { output } = result
    assert.deepEqual(output.map((entry) => entry.fileName).sort(), ['index.html', 'sw.js'])

    const html = output.find((entry) => entry.fileName === 'index.html').source.toString()
    assert.match(html, /<script type="module">\s*\S/)
    assert.match(html, /<style>\s*\S/)
    assert.doesNotMatch(html, /<script\b[^>]*\bsrc=/i)
    assert.doesNotMatch(html, /<link\b[^>]*\brel="(?:stylesheet|modulepreload)"/i)
    assert(!html.includes('%CORNERSTONE_FAVICON%'))
    for (const name of ['logo_black.jpg', 'logo_white.jpg']) {
        const logo = await readFile(new URL(`../public/${name}`, import.meta.url))
        assert(html.includes(`data:image/jpeg;base64,${logo.toString('base64')}`), `${name} stays inline`)
    }

    const worker = output.find((entry) => entry.fileName === 'sw.js').source.toString()
    assert.equal(worker, await readFile(new URL('../public/sw.js', import.meta.url), 'utf8'))
})
