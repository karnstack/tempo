import path from "path"
import process from "node:process"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { tanstackRouter } from "@tanstack/router-plugin/vite"
import { defineConfig } from "vite"

// Port resolution mirrors the Go server's resolveListen():
//   PORT          - set by portless.sh / Heroku-style wrappers.
//   default 4810  - sits inside portless's 4000-4999 assignment range
//                   so the experience is consistent with or without
//                   portless, and well clear of the usual
//                   3000/5173/8080 dev-port pile-up.
// The dev proxy target (`/api` -> Go server) is configurable via
// VITE_API_TARGET so portless can point it at the Go subdomain
// (e.g. https://api.tempo.localhost) when both apps run behind it.
const PORT = Number(process.env.PORT ?? 4810)
const API_TARGET = process.env.VITE_API_TARGET ?? "http://localhost:4811"

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    tanstackRouter({ target: "react", autoCodeSplitting: true }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: PORT,
    strictPort: true,
    proxy: {
      "/api": { target: API_TARGET, changeOrigin: true },
    },
  },
})
