import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
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
