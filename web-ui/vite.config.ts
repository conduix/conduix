import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        ws: true, // WebSocket proxy (LSP)
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
    rollupOptions: {
      output: {
        // 무거운 라이브러리를 별도 청크로 분리해 초기 번들 크기를 줄인다.
        // Vite 8(rolldown)은 manualChunks를 함수 형태로만 받는다.
        manualChunks(id) {
          if (id.includes('node_modules')) {
            if (/[/\\]@mui[/\\]/.test(id)) return 'mui'
            if (/[/\\]@monaco-editor[/\\]|[/\\]monaco-editor[/\\]/.test(id)) return 'editor'
            if (/[/\\]@xyflow[/\\]|[/\\]dagre[/\\]/.test(id)) return 'flow'
            if (/[/\\]react(-dom|-router-dom)?[/\\]/.test(id)) return 'react'
          }
        },
      },
    },
  },
})
