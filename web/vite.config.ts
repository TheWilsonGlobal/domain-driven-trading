import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// https://vitejs.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '..', '');
  const port = Number(env.WEB_PORT) || 5471;
  const gatewayPort = Number(env.PORT) || 5470;

  return {
    plugins: [react(), tailwindcss()],
    server: {
      port,
      host: true,
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
  };
});
