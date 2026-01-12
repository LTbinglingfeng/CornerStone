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

export default defineConfig({
  plugins: [cornerstoneInlineLogos(), react()],
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
