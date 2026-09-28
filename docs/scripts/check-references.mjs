import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

const read = (path) => JSON.parse(readFileSync(new URL(`../src/data/${path}`, import.meta.url), "utf8"));
const config = read("config.json");
const api = read("api.json");
const commands = new Set(read("cli.json").map((command) => command.path));
const guides = new URL("../src/content/guides/", import.meta.url).pathname;

const keys = new Set();
const bindings = new Set();
const settings = new Set();
const leaves = new Set();
const collect = (section) => {
  if (section.path) settings.add(section.path);
  for (const setting of section.settings) {
    settings.add(setting.path);
    if (setting.key.includes("_")) leaves.add(setting.key);
    if (!setting.path.startsWith("keybindings.")) continue;
    bindings.add(setting.path.slice("keybindings.".length));
    for (const key of JSON.parse(setting.default)) keys.add(key);
  }
  section.sections.forEach(collect);
};
collect(config);

const fields = new Set();
for (const [message, { fields: list }] of Object.entries(api.messages)) {
  for (const field of list) fields.add(`${message}.${field.name}`);
}
const fieldNames = new Set([...fields].map((field) => field.split(".").pop()).filter((name) => name.includes("_")));

const found = [];
for (const file of readdirSync(guides).filter((name) => /\.mdx?$/.test(name))) {
  const text = readFileSync(join(guides, file), "utf8");
  const at = (index) => `${file}:${String(text.slice(0, index).split("\n").length)}`;

  for (const match of text.matchAll(/`([^`\n]+)`/g)) {
    const code = match[1];
    const bare = code.replace(/^\[(.*)\]$/, "$1");
    if (keys.has(code)) found.push(`${at(match.index)}: \`${code}\` is a keybinding; use <Key name="..." />`);
    else if (settings.has(bare) || leaves.has(bare)) {
      found.push(`${at(match.index)}: \`${code}\` is a config setting; use <Setting path="..." />`);
    } else if (
      commands.has(code.split(" ").slice(0, 3).join(" ")) ||
      commands.has(code.split(" ").slice(0, 2).join(" "))
    ) {
      found.push(`${at(match.index)}: \`${code}\` is a CLI command; use <Command path="..." />`);
    } else if (fieldNames.has(code)) {
      found.push(`${at(match.index)}: \`${code}\` is an API field; use <Field message="..." name="..." />`);
    }
  }
  for (const match of text.matchAll(/\]\((?:\.\.\/)+(config|api)\/[^)]*#[^)]*\)/g)) {
    found.push(`${at(match.index)}: link to the ${match[1]} reference by hand; use <Setting>, <Key> or <Field>`);
  }
  for (const match of text.matchAll(/<Key name="([^"]+)"/g)) {
    if (!bindings.has(match[1])) found.push(`${at(match.index)}: keybindings.${match[1]} is not in config.json`);
  }
  for (const match of text.matchAll(/<Setting path="([^"]+)"/g)) {
    if (!settings.has(match[1])) found.push(`${at(match.index)}: ${match[1]} is not in config.json`);
  }
  for (const match of text.matchAll(/<Command path="([^"]+)"/g)) {
    if (!commands.has(match[1] ? `komrad ${match[1]}` : "komrad"))
      found.push(`${at(match.index)}: komrad ${match[1]} is not in cli.json`);
  }
  for (const match of text.matchAll(/<Field message="([^"]+)" name="([^"]+)"/g)) {
    if (!fields.has(`${match[1]}.${match[2]}`))
      found.push(`${at(match.index)}: ${match[1]}.${match[2]} is not in api.json`);
  }
}

if (found.length > 0) {
  console.error(found.join("\n"));
  process.exit(1);
}
