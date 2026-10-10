import type { CollectionEntry } from "astro:content";
import { commandBySlug, messageOf, referencedTypes } from "./reference";
import { commandOf, fieldOf, keybindingOf, settingOf } from "./reference/refs";
import type { Command, Enum, Field, Flag, Message, Method, Service } from "./reference/types";
import { link } from "./site";

export const absolute = (path: string) => new URL(link(path), import.meta.env.SITE).href;

export const guideMarkdownPath = (id: string) => `/guides/${id}.md`;
export const commandMarkdownPath = (slug: string) => (slug === "" ? "/cli.md" : `/cli/${slug}.md`);
export const methodMarkdownPath = (service: Service, method: Method) => `/api/${service.slug}/${method.slug}.md`;

const indented = (line: string) => /^(\t| {2,})/.test(line);

function prose(text: string) {
  return text
    .split(/\n\s*\n/)
    .filter((block) => block.trim() !== "")
    .map((block) => {
      const lines = block.split("\n").filter((line) => line.trim() !== "");
      if (!lines.every(indented)) return block.trim();
      return ["```", ...lines.map((line) => line.replace(/^(\t| {2,4})/, "")), "```"].join("\n");
    })
    .join("\n\n");
}

const cell = (text: string) => text.replace(/\s*\n\s*/g, " ").replaceAll("|", "\\|");

function table(headings: string[], rows: string[][]) {
  return [
    `| ${headings.join(" | ")} |`,
    `| ${headings.map(() => "---").join(" | ")} |`,
    ...rows.map((row) => `| ${row.map(cell).join(" | ")} |`),
  ].join("\n");
}

function attributes(tag: string) {
  const found: Record<string, string> = {};
  for (const [, name, quoted, braced] of tag.matchAll(/(\w+)=(?:"([^"]*)"|\{(\d+)\})/g)) {
    if (name) found[name] = quoted ?? braced ?? "";
  }
  return found;
}

const code = (text: string) => `\`${text}\``;

function inline(name: string, tag: string) {
  const { path = "", args, label, at, message = "" } = attributes(tag);
  switch (name) {
    case "Command":
      return code([commandOf(path).title, args].filter(Boolean).join(" "));
    case "Setting":
      settingOf(path);
      return code(label ?? path);
    case "Field":
      fieldOf(message, attributes(tag).name ?? "");
      return code(attributes(tag).name ?? "");
    case "Key": {
      const keys = keybindingOf(attributes(tag).name ?? "").keys;
      const shown = at === undefined ? keys : keys.slice(Number(at), Number(at) + 1);
      return shown.map(code).join(" or ");
    }
    default:
      throw new Error(`no markdown form for <${name}>`);
  }
}

function figure(block: string) {
  const media = (pattern: RegExp) => {
    const name = pattern.exec(block)?.[1];
    if (!name) throw new Error(`figure names no media: ${block}`);
    return new URL(`/media/${name}`, import.meta.env.SITE).href;
  };
  const label = /(?:aria-label|alt)="([^"]*)"/.exec(block)?.[1] ?? "";
  return block.includes("<video")
    ? `[Video: ${label}](${media(/media\("([^"]+\.mp4)"\)/)})`
    : `![${label}](${media(/media\("([^"]+)"\)/)})`;
}

function alert(tag: string, body: string) {
  const { variant = "info", title } = attributes(tag);
  const lead = [variant.charAt(0).toUpperCase() + variant.slice(1), title].filter(Boolean).join(": ");
  const lines = body
    .trim()
    .split("\n")
    .map((line) => line.trim());
  return [`> ${lead}`, ">", ...lines.map((line) => (line === "" ? ">" : `> ${line}`))].join("\n");
}

function rewriteLinks(text: string, page: URL) {
  const guides = new URL(link("/guides"), import.meta.env.SITE).pathname;
  return text.replace(/\]\((\.[^)\s]*)\)/g, (_, href: string) => {
    const target = new URL(href, page);
    const guide = new RegExp(`^${guides}([^/]+)/$`).exec(target.pathname)?.[1];
    return `](${guide ? `${absolute(guideMarkdownPath(guide))}${target.hash}` : target.href})`;
  });
}

function rewriteProse(text: string, page: URL) {
  const rewritten = text
    .replace(/^import .*\n/gm, "")
    .replace(/<figure>[\s\S]*?<\/figure>/g, figure)
    .replace(/<Alert([^>]*)>([\s\S]*?)<\/Alert>/g, (_, tag: string, body: string) => alert(tag, body))
    .replace(
      /<(Command|Setting|Field)\b(?:"[^"]*"|[^>"/])*>([\s\S]*?)<\/\1>/g,
      (_, _name: string, children: string) => children,
    )
    .replace(/<(Command|Setting|Field|Key)\b((?:"[^"]*"|[^>"])*)\/>/g, (_, name: string, tag: string) =>
      inline(name, tag),
    )
    .replace(
      /<a href=\{link\(["']([^"']+)["']\)\}>([^<]*)<\/a>/g,
      (_, path: string, text: string) => `[${text}](${absolute(path)})`,
    )
    .replace(/<a href="([^"]+)">([^<]*)<\/a>/g, (_, href: string, text: string) => `[${text}](${href})`)
    .replaceAll('{" "}', " ");
  return rewriteLinks(rewritten, page);
}

export function guideMarkdown(guide: CollectionEntry<"guides">) {
  const page = new URL(link(`/guides/${guide.id}`), import.meta.env.SITE);
  const body = (guide.body ?? "")
    .split(/(^```[\s\S]*?^```$)/m)
    .map((part, index) => (index % 2 === 1 ? part : rewriteProse(part, page)))
    .join("")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
  return [`# ${guide.data.title}`, guide.data.description, body].filter(Boolean).join("\n\n") + "\n";
}

const flagTable = (flags: Flag[]) =>
  table(
    ["Flag", "Type", "Description"],
    flags.map((flag) => [
      code(`${flag.shorthand ? `-${flag.shorthand}, ` : ""}--${flag.name}`),
      flag.type,
      [flag.usage, flag.default && flag.default !== "[]" && flag.default !== "false" ? `(default ${flag.default})` : ""]
        .filter(Boolean)
        .join(" "),
    ]),
  );

export function commandMarkdown(command: Command) {
  const children = command.children.flatMap((slug) => commandBySlug.get(slug) ?? []);
  const normalize = (text: string) => text.trim().replace(/\.$/, "");
  const paragraphs = command.long.split(/\n\s*\n/);
  const long =
    normalize(paragraphs[0] ?? "") === normalize(command.short) ? paragraphs.slice(1).join("\n\n") : command.long;

  return (
    [
      `# ${command.path}`,
      command.short,
      ["```sh", command.usage, "```"].join("\n"),
      long && prose(long),
      command.slug === "rpc" &&
        `Each subcommand is one endpoint, documented with its request and response in the [API reference](${absolute("/llms.txt")}).`,
      command.aliases.length > 0 && `Also answers to ${command.aliases.map(code).join(", ")}.`,
      command.example && ["## Examples", ["```sh", command.example.trimEnd(), "```"].join("\n")].join("\n\n"),
      children.length > 0 &&
        [
          "## Commands",
          children
            .map((child) => `- [${child.path}](${absolute(commandMarkdownPath(child.slug))}): ${child.short}`)
            .join("\n"),
        ].join("\n\n"),
      command.flags.length > 0 && ["## Flags", flagTable(command.flags)].join("\n\n"),
      command.inherited.length > 0 && ["## Inherited flags", flagTable(command.inherited)].join("\n\n"),
    ]
      .filter(Boolean)
      .join("\n\n") + "\n"
  );
}

function notes(field: Field) {
  const found = [...field.rules, ...(field.oneof ? [`one of ${field.oneof}`] : [])];
  return found.length > 0 ? `(${found.join(", ")})` : "";
}

function messageTable(name: string, message: Message) {
  const rows = message.fields.map((field) => [
    code(field.name),
    `${field.map ? "map of " : field.repeated ? "list of " : ""}${field.type}`,
    [field.description, notes(field)].filter(Boolean).join(" "),
  ]);
  return [
    `### ${message.name}`,
    code(name),
    message.description && prose(message.description),
    rows.length === 0 ? "No fields." : table(["Field", "Type", "Description"], rows),
  ]
    .filter(Boolean)
    .join("\n\n");
}

function enumTable(name: string, value: Enum) {
  return [
    `### ${value.name}`,
    code(name),
    value.description && prose(value.description),
    table(
      ["Value", "Description"],
      value.values.map((entry) => [code(entry.name), entry.description]),
    ),
  ]
    .filter(Boolean)
    .join("\n\n");
}

export function methodMarkdown(service: Service, method: Method) {
  const types = referencedTypes([method.request, method.response]);
  const nested = types.messages.filter(([name]) => name !== method.request && name !== method.response);
  const details = method.description.slice(method.summary.length).trim();
  const traits = [
    method.readOnly ? "read-only" : "writes",
    method.idempotent ? "idempotent" : "not idempotent",
    ...(method.destructive ? ["destructive"] : []),
    method.mcp ? "MCP tool" : "CLI only",
  ];
  const offered = method.roles.length > 0 ? `, offered to ${method.roles.join(", ")} agents` : "";
  const calling = [
    `- Connect: \`POST http://127.0.0.1:8787${method.path}\` with a JSON body`,
    `- CLI: \`${method.command}\``,
    ...(method.mcp ? [`- MCP: the \`${method.tool}\` tool on the komrad MCP server${offered}`] : []),
  ].join("\n");

  return (
    [
      `# ${method.tool}`,
      method.summary,
      `${service.title} service (\`${service.name}\`): ${traits.join(", ")}.`,
      details && prose(details),
      ["## Calling it", calling].join("\n\n"),
      ["## Request", messageTable(method.request, messageOf(method.request))].join("\n\n"),
      [
        "## Response",
        method.response === method.request
          ? `The same ${messageOf(method.request).name} as the request.`
          : messageTable(method.response, messageOf(method.response)),
      ].join("\n\n"),
      (nested.length > 0 || types.enums.length > 0) &&
        [
          "## Types",
          ...nested.map(([name, message]) => messageTable(name, message)),
          ...types.enums.map(([name, value]) => enumTable(name, value)),
        ].join("\n\n"),
    ]
      .filter(Boolean)
      .join("\n\n") + "\n"
  );
}
