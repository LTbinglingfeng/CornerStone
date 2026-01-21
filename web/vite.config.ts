import fs from 'node:fs'
import { fileURLToPath } from 'node:url'
import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'

const readJpegDataUrl = (filePath: string) => {
  const base64 = fs.readFileSync(filePath).toString('base64')
  return `data:image/jpeg;base64,${base64}`
}

const cornerstoneInlineLogos = (): Plugin => {
  const virtualId = 'virtual:cornerstone-logos'
  const resolvedVirtualId = `\0${virtualId}`

  const logoWhitePath = fileURLToPath(new URL('./public/logo_white.jpg', import.meta.url))
  const logoBlackPath = fileURLToPath(new URL('./public/logo_black.jpg', import.meta.url))

  let logoWhiteDataUrl = ''
  let logoBlackDataUrl = ''

  const loadLogos = () => {
    logoWhiteDataUrl = readJpegDataUrl(logoWhitePath)
    logoBlackDataUrl = readJpegDataUrl(logoBlackPath)
  }

  loadLogos()

  return {
    name: 'cornerstone-inline-logos',
    resolveId(id) {
      if (id === virtualId) return resolvedVirtualId
      return null
    },
    load(id) {
      if (id !== resolvedVirtualId) return null
      return [
        `export const logoWhiteDataUrl = ${JSON.stringify(logoWhiteDataUrl)}`,
        `export const logoBlackDataUrl = ${JSON.stringify(logoBlackDataUrl)}`,
        '',
      ].join('\n')
    },
    transformIndexHtml(html) {
      return html.replaceAll('%CORNERSTONE_FAVICON%', logoBlackDataUrl)
    },
    handleHotUpdate({ file, server }) {
      if (file === logoWhitePath || file === logoBlackPath) {
        loadLogos()
        server.ws.send({ type: 'full-reload' })
      }
      return []
    },
  }
}

const normalizeViteAssetPath = (url: string) => {
  if (url.startsWith('data:') || url.startsWith('http:') || url.startsWith('https:') || url.startsWith('//')) {
    return null
  }

  const withoutQuery = url.split(/[?#]/)[0]
  return withoutQuery.replace(/^\/+/, '')
}

const escapeInlineTag = (content: string, tagName: 'script' | 'style') => {
  return content.replaceAll(`</${tagName}>`, `<\\/${tagName}>`)
}

const cornerstoneSingleFileBuild = (): Plugin => {
  return {
    name: 'cornerstone-single-file-build',
    apply: 'build',
    enforce: 'post',
    generateBundle(_outputOptions, bundle) {
      const htmlEntry = Object.values(bundle).find(
        (entry) => entry.type === 'asset' && entry.fileName === 'index.html'
      )
      if (!htmlEntry) return

      const htmlAsset = htmlEntry as { source: string | Uint8Array }
      const originalHtml = typeof htmlAsset.source === 'string'
        ? htmlAsset.source
        : Buffer.from(htmlAsset.source).toString('utf8')

      let html = originalHtml

      html = html.replaceAll(/<link\b[^>]*rel="modulepreload"[^>]*>/g, '')

      html = html.replaceAll(/<link\b[^>]*rel="stylesheet"[^>]*href="([^"]+)"[^>]*>/g, (tag, href) => {
        const fileName = normalizeViteAssetPath(href)
        if (!fileName) return tag

        const entry = bundle[fileName]
        if (!entry) return tag

        const css = entry.type === 'asset'
          ? (typeof entry.source === 'string' ? entry.source : Buffer.from(entry.source).toString('utf8'))
          : entry.code

        return `<style>\n${escapeInlineTag(css, 'style')}\n</style>`
      })

      html = html.replaceAll(/<script\b[^>]*type="module"[^>]*src="([^"]+)"[^>]*><\/script>/g, (tag, src) => {
        const fileName = normalizeViteAssetPath(src)
        if (!fileName) return tag

        const entry = bundle[fileName]
        if (!entry) return tag

        const js = entry.type === 'chunk'
          ? entry.code
          : (typeof entry.source === 'string' ? entry.source : Buffer.from(entry.source).toString('utf8'))

        return `<script type="module">\n${escapeInlineTag(js, 'script')}\n</script>`
      })

      htmlAsset.source = html

      for (const fileName of Object.keys(bundle)) {
        if (fileName !== 'index.html') {
          delete bundle[fileName]
        }
      }
    },
  }
}

export default defineConfig({
  plugins: [cornerstoneInlineLogos(), react(), cornerstoneSingleFileBuild()],
  build: {
    copyPublicDir: true,
    rollupOptions: {
      output: {
        inlineDynamicImports: true,
      },
    },
  },
  server: {
    host: '0.0.0.0',
    port: 3000,
    open: true,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:1205',
        changeOrigin: true
      },
      '/management': {
        target: 'http://127.0.0.1:1205',
        changeOrigin: true
      }
    }
  },
  preview: {
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:1205',
        changeOrigin: true
      },
      '/management': {
        target: 'http://127.0.0.1:1205',
        changeOrigin: true
      }
    }
  }
})
