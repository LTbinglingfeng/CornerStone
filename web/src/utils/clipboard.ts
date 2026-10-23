/** Copy on HTTPS and on self-hosted HTTP pages where Clipboard API is unavailable. */
export async function copyText(text: string): Promise<void> {
    if (navigator.clipboard?.writeText) {
        try {
            await navigator.clipboard.writeText(text)
            return
        } catch {
            // Permission denial can still allow the user-initiated legacy copy path.
        }
    }

    const activeElement = document.activeElement
    const selection = document.getSelection()
    const ranges = selection
        ? Array.from({ length: selection.rangeCount }, (_, index) => selection.getRangeAt(index).cloneRange())
        : []
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.readOnly = true
    textarea.style.cssText = 'position:fixed;top:0;left:-9999px;opacity:0'
    document.body.appendChild(textarea)
    try {
        textarea.focus({ preventScroll: true })
        textarea.select()
        if (!document.execCommand('copy')) throw new Error('Clipboard copy unavailable')
    } finally {
        textarea.remove()
        if (activeElement instanceof HTMLElement) activeElement.focus({ preventScroll: true })
        if (selection) {
            selection.removeAllRanges()
            for (const range of ranges) selection.addRange(range)
        }
    }
}
