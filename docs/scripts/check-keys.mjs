import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

const config = JSON.parse(readFileSync(new URL("../src/data/config.json", import.meta.url), "utf8"));
const guides = new URL("../src/content/guides/", import.meta.url).pathname;

const keys = new Set();
const names = new Set();
const collect = (section) => {
  for (const setting of section.settings) {
    if (!setting.path.startsWith("keybindings.")) continue;
    names.add(setting.path.slice("keybindings.".length));
    for (const key of JSON.parse(setting.default)) keys.add(key);
  }
  section.sections.forEach(collect);
};
collect(config);

const found = [];
for (const file of readdirSync(guides).filter((name) => /\.mdx?$/.test(name))) {
  readFileSync(join(guides, file), "utf8")
    .split("\n")
    .forEach((line, i) => {
      for (const [, code] of line.matchAll(/`([^`]+)`/g)) {
        if (keys.has(code)) found.push(`${file}:${String(i + 1)}: \`${code}\` is a keybinding; use <Key name="..." />`);
      }
      for (const [, name] of line.matchAll(/<Key name="([^"]+)"/g)) {
        if (!names.has(name)) found.push(`${file}:${String(i + 1)}: keybindings.${name} is not in config.json`);
      }
    });
}

if (found.length > 0) {
  console.error(found.join("\n"));
  process.exit(1);
}
