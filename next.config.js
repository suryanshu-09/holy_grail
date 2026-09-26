/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // Standalone output for a minimal production Docker image (PLAN4 Phase 25).
  // Produces .next/standalone/server.js plus static assets; see Dockerfile.web.
  output: 'standalone',
};

module.exports = nextConfig;
