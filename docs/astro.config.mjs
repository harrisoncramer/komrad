import { defineConfig } from "astro/config";
import mdx from "@astrojs/mdx";
import react from "@astrojs/react";
import tailwindcss from "@tailwindcss/vite";
import { createReadStream, existsSync, statSync } from "node:fs";
import { basename, extname } from "node:path";

const version = process.env.PUBLIC_DOCS_VERSION || "dev";
const base = `/${version}/`;

const pagefindDir = new URL("./dist/pagefind/", import.meta.url);

const absoluteGuideLinks = () => (tree, file) => {
  const page = new URL(`guides/${basename(file.path, extname(file.path))}/`, `http://docs${base}`);
  const visit = (node) => {
    const href = node.properties?.href;
    if (node.tagName === "a" && typeof href === "string" && href.startsWith(".")) {
      const target = new URL(href, page);
      node.properties.href = `${target.pathname}${target.hash}`;
    }
    node.children?.forEach(visit);
  };
  visit(tree);
};

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
    const serve = (req, res, next) => {
      const requested = new URL("." + req.url.split("?")[0], pagefindDir);
      if (!requested.href.startsWith(pagefindDir.href)) return next();
      if (!existsSync(requested) || !statSync(requested).isFile()) return next();
      const type = pagefindTypes[extname(requested.pathname)];
      if (type) res.setHeader("Content-Type", type);
      createReadStream(requested).pipe(res);
    };
    server.middlewares.use(`${base}pagefind`, serve);
    server.middlewares.use("/pagefind", serve);
  },
};

export default defineConfig({
  base,
  trailingSlash: "ignore",
  integrations: [mdx(), react()],
  vite: {
    plugins: [tailwindcss(), pagefind],
  },
  markdown: {
    rehypePlugins: [absoluteGuideLinks],
    shikiConfig: {
      theme: "tokyo-night",
    },
  },
});
