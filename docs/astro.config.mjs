import { defineConfig } from "astro/config";
import mdx from "@astrojs/mdx";
import react from "@astrojs/react";
import tailwindcss from "@tailwindcss/vite";
import { createReadStream, existsSync, statSync } from "node:fs";
import { extname } from "node:path";

const version = process.env.PUBLIC_DOCS_VERSION || "dev";
const base = version === "dev" ? "/" : `/${version}/`;

const pagefindDir = new URL("./dist/pagefind/", import.meta.url);

const pagefindTypes = {
  ".js": "application/javascript",
  ".json": "application/json",
  ".css": "text/css",
  ".pagefind": "application/wasm",
};

const pagefind = {
  name: "pagefind",
  resolveId(id) {
    if (id.endsWith("/pagefind/pagefind.js")) {
      return { id, external: true };
    }
  },
  configureServer(server) {
    server.middlewares.use(`${base}pagefind`, (req, res, next) => {
      const requested = new URL("." + req.url.split("?")[0], pagefindDir);
      if (!requested.href.startsWith(pagefindDir.href)) return next();
      if (!existsSync(requested) || !statSync(requested).isFile()) return next();
      const type = pagefindTypes[extname(requested.pathname)];
      if (type) res.setHeader("Content-Type", type);
      createReadStream(requested).pipe(res);
    });
  },
};

export default defineConfig({
  base,
  integrations: [mdx(), react()],
  vite: {
    plugins: [tailwindcss(), pagefind],
  },
  markdown: {
    shikiConfig: {
      theme: "tokyo-night",
    },
  },
});
