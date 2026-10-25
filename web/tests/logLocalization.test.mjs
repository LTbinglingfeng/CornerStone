// Node 24: node --experimental-strip-types --test tests/logLocalization.test.mjs
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readFileSync, readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { join } from 'node:path'
import { localizeLogEvent, localizeLogField, localizeLogValue } from '../src/components/LogsPage/logLocalization.ts'
import { formatFieldValue, fieldTone } from '../src/components/LogsPage/logFormat.ts'
import { logVocabularyZh, logVocabularyEn } from '../src/i18n/logVocabulary.ts'

const value = (key, value, locale = 'zh') => localizeLogValue({ key, value }, locale)

test('HTTP screenshot vocabulary and both locales', () => {
    assert.equal(localizeLogEvent('http.request.started', 'zh'), 'HTTP 请求开始')
    assert.equal(localizeLogEvent('http.request.completed', 'zh'), 'HTTP 请求结束')
    for (const key of ['duration_ms', 'request', 'response_bytes', 'method', 'trigger']) {
        assert.notEqual(localizeLogField(key, 'zh'), key)
        assert.equal(localizeLogField(key, 'en'), key)
    }
    assert.equal(value('trigger', 'http_request'), 'HTTP 请求')
    assert.equal(value('trigger', 'http_request', 'en'), 'http_request')
    assert.equal(value('method', 'GET'), '获取（GET）')
    assert.equal(value('status', 'write_failed'), '写入失败')
    assert.equal(value('usage', 'reported'), '已报告')
    assert.equal(value('stream', 'false'), '否')
    assert.equal(value('phase', 'sending'), '发送中')
    assert.equal(value('status', 'queue_committing'), '队列：写入中')
})

test('unknown data, free text, paths and identifiers are never translated', () => {
    for (const key of [
        'request',
        'session',
        'provider',
        'model',
        'tool',
        'call',
        'path',
        'error',
        'err',
        'operation',
    ]) {
        for (const text of ['failed', 'http_request', 'true', '/api/failed', 'timeout: upstream failed']) {
            assert.equal(value(key, text), text)
        }
    }
    assert.equal(value('status', 'future_status'), 'future_status')
    assert.equal(value('future_field', 'failed'), 'failed')
    for (const unknown of ['future.event', 'constructor', '__proto__', 'toString']) {
        assert.equal(localizeLogEvent(unknown, 'zh'), unknown)
        assert.equal(localizeLogField(unknown, 'zh'), unknown)
        assert.equal(value(unknown, 'failed'), 'failed')
        assert.equal(value('status', unknown), unknown)
    }
})

test('display localization leaves raw metadata and severity intact', () => {
    const field = Object.freeze({ key: 'status', value: 'failed' })
    assert.equal(localizeLogValue(field, 'zh'), '失败')
    assert.equal(field.value, 'failed')
    assert.equal(fieldTone(field), 'danger')
    assert.equal(formatFieldValue({ key: 'duration_ms', value: '1500' }, true), '1.5 s')
    assert.equal(formatFieldValue({ key: 'response_bytes', value: '2048' }, true), '2.0 KB')
    const id = '12345678-abcd-abcd-abcd-123456789012'
    assert.equal(value('request', id), id)
    assert.equal(formatFieldValue({ key: 'request', value: id }), id)
    assert.equal(formatFieldValue({ key: 'request', value: id }, true), '12345678')
    assert.equal(value('status', ''), '')
})

test('every currently emitted structured event has Chinese and original English copy', () => {
    const root = fileURLToPath(new URL('../../', import.meta.url))
    let checked = 0
    for (const dir of ['api', 'client', 'storage']) {
        for (const file of readdirSync(join(root, dir))) {
            if (!file.endsWith('.go') || file.endsWith('_test.go')) continue
            const source = readFileSync(join(root, dir, file), 'utf8')
            for (const [, event] of source.matchAll(/logging\.Event\([^,\n]+,\s*"([^"]+)"/g)) {
                assert.ok(Object.hasOwn(logVocabularyZh.events, event), `${file}: ${event}`)
                assert.notEqual(localizeLogEvent(event, 'zh'), event)
                assert.equal(localizeLogEvent(event, 'en'), event)
                checked++
            }
        }
    }
    assert.ok(checked > 50)
    assert.deepEqual(Object.keys(logVocabularyZh.fields), Object.keys(logVocabularyEn.fields))
})
