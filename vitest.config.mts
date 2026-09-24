import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

// NOTE: this file is intentionally `.mts` (not `.ts`) so that `next build`
// type-checking — driven by tsconfig `include: ["**/*.ts", "**/*.tsx"]` —
// does not try to resolve `@vitejs/plugin-react` types under
// `moduleResolution: "node"`. Vitest auto-discovers `vitest.config.mts`.
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.ts'],
    include: ['__tests__/**/*.test.{ts,tsx}'],
  },
});
