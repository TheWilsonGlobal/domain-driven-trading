import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// https://vitejs.dev/config/
export default defineConfig(({ mode }) => {
  // Only WEB_PORT and PORT are read from ../.env; the prefix filter keeps every
  // other variable (DB/Redis/Supabase secrets) out of this config entirely.
  const raw = loadEnv(mode, '..', ['WEB_PORT', 'PORT']);
  const port = Number(raw.WEB_PORT) || 5471;
  const gatewayPort = Number(raw.PORT) || 5470;

  return {
    plugins: [react(), tailwindcss()],
    server: {
      port,
      host: 'localhost',
      proxy: {
        '/v1': {
          target: `http://localhost:${gatewayPort}`,
          changeOrigin: true,
        },
        '/ws': {
          target: `ws://localhost:${gatewayPort}`,
          ws: true,
        },
      },
    },
    // `vite preview` runs on Render (ddd-web). Vite 5.4 rejects any Host it does not list.
    preview: {
      allowedHosts: ['.onrender.com'],
    },
  };
});
