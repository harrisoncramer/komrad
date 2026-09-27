import apiData from "../data/api.json";
import cliData from "../data/cli.json";
import configData from "../data/config.json";
import { link } from "../site";
import type { API, Command, Message, Section } from "./types";

export const api = apiData as API;
export const commands = cliData as Command[];
export const config = configData as Section;

export function messageOf(name: string): Message {
  const message = api.messages[name];
  if (!message) throw new Error(`api.json names ${name} but does not describe it`);
  return message;
}

export const commandBySlug = new Map(commands.map((command) => [command.slug, command]));

export const commandHref = (slug: string) => link(slug === "" ? "/cli" : `/cli/${slug}`);

export function referencedTypes(roots: string[]) {
  const messages: [string, Message][] = [];
  const enums = new Set<string>();
  const seen = new Set<string>();
  const queue = [...roots];

  for (let name = queue.shift(); name !== undefined; name = queue.shift()) {
    if (seen.has(name)) continue;
    seen.add(name);

    const message = api.messages[name];
    if (!message) continue;
    messages.push([name, message]);

    for (const field of message.fields) {
      if (!field.ref) continue;
      if (api.enums[field.ref]) enums.add(field.ref);
      else queue.push(field.ref);
    }
  }

  return {
    messages,
    enums: [...enums].flatMap((name) => {
      const value = api.enums[name];
      return value ? [[name, value] as const] : [];
    }),
  };
}

export const anchor = (name: string) => name.replaceAll(".", "-");

const escape = (text: string) =>
  text.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");

const inline = (text: string) => escape(text).replace(/`([^`]+)`/g, "<code>$1</code>");

const indented = (line: string) => /^(\t| {2,})/.test(line);

export const prose = (text: string) =>
  text
    .split(/\n\s*\n/)
    .filter((block) => block.trim() !== "")
    .map((block) => {
      const lines = block.split("\n").filter((line) => line.trim() !== "");
      if (lines.every(indented)) {
        const code = lines.map((line) => line.replace(/^(\t| {2,4})/, "")).join("\n");
        return `<pre class="plain-code"><code>${escape(code)}</code></pre>`;
      }

      return `<p>${inline(block.trim())}</p>`;
    })
    .join("");

export const kebab = (name: string) =>
  name
    .replace(/([a-z0-9])([A-Z])/g, "$1-$2")
    .replace(/_/g, "-")
    .toLowerCase();
