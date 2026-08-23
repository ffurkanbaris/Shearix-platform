import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  poweredByHeader: false,
  // `npm run lint` is the explicit CI command. Keep the production build from
  // running a second, framework-specific lint pass against the flat config.
  eslint: { ignoreDuringBuilds: true },
};

export default nextConfig;
