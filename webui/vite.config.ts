import path from 'path';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

const linkedAliases = [
  { find: /^@assistant-ui\/react$/, replacement: path.resolve(__dirname, 'node_modules/@assistant-ui/react') },
  { find: /^@tanstack\/react-query$/, replacement: path.resolve(__dirname, 'node_modules/@tanstack/react-query') },
  { find: /^class-variance-authority$/, replacement: path.resolve(__dirname, 'node_modules/class-variance-authority') },
  { find: /^clsx$/, replacement: path.resolve(__dirname, 'node_modules/clsx') },
  { find: /^lucide-react$/, replacement: path.resolve(__dirname, 'node_modules/lucide-react') },
  { find: /^radix-ui$/, replacement: path.resolve(__dirname, 'node_modules/radix-ui') },
  { find: /^react$/, replacement: path.resolve(__dirname, 'node_modules/react') },
  { find: /^react\/jsx-runtime$/, replacement: path.resolve(__dirname, 'node_modules/react/jsx-runtime.js') },
  { find: /^react\/jsx-dev-runtime$/, replacement: path.resolve(__dirname, 'node_modules/react/jsx-dev-runtime.js') },
  { find: /^react-dom$/, replacement: path.resolve(__dirname, 'node_modules/react-dom') },
  { find: /^react-dom\/client$/, replacement: path.resolve(__dirname, 'node_modules/react-dom/client.js') },
  { find: /^react-hook-form$/, replacement: path.resolve(__dirname, 'node_modules/react-hook-form') },
  { find: /^tailwind-merge$/, replacement: path.resolve(__dirname, 'node_modules/tailwind-merge') }
];

export default defineConfig({
  base: '/',
  plugins: [react(), tailwindcss()],
  resolve: { preserveSymlinks: true, alias: [...linkedAliases, { find: '@', replacement: path.resolve(__dirname, './src') }] },
  build: {
    target: 'esnext',
    modulePreload: false,
    cssCodeSplit: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('/node_modules/@assistant-ui/') || id.includes('/node_modules/assistant-')) return 'assistant-ui';
          if (/\/node_modules\/(react|react-dom|react-router|scheduler)\//.test(id)) return 'react';
          if (id.includes('/node_modules/libphonenumber-js/')) return 'phone';
          return undefined;
        },
      },
    },
  },
});
