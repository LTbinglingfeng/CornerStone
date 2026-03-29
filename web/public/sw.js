self.addEventListener('install', (event) => {
    event.waitUntil(self.skipWaiting())
})

self.addEventListener('activate', (event) => {
    event.waitUntil(self.clients.claim())
})

function buildNotificationTargetUrl(sessionId, promptId) {
    const params = new URLSearchParams()
    if (typeof sessionId === 'string' && sessionId.trim() !== '') {
        params.set('sessionId', sessionId.trim())
    }
    if (typeof promptId === 'string' && promptId.trim() !== '') {
        params.set('promptId', promptId.trim())
    }
    const search = params.toString()
    return `/${search ? `?${search}` : ''}`
}

self.addEventListener('notificationclick', (event) => {
    const notification = event.notification
    notification.close()

    const data = notification.data || {}
    const sessionId = typeof data.sessionId === 'string' ? data.sessionId : ''
    const promptId = typeof data.promptId === 'string' ? data.promptId : ''
    const targetUrl = buildNotificationTargetUrl(sessionId, promptId)

    event.waitUntil(
        (async () => {
            const clientList = await self.clients.matchAll({
                type: 'window',
                includeUncontrolled: true,
            })

            for (const client of clientList) {
                if (!('focus' in client)) {
                    continue
                }

                await client.focus()
                client.postMessage({
                    type: 'OPEN_SESSION',
                    sessionId,
                    promptId,
                })
                return
            }

            if ('openWindow' in self.clients) {
                await self.clients.openWindow(targetUrl)
            }
        })()
    )
})
