import { anchor, api, commandBySlug, commandHref, config, referencedTypes } from ".";
import { link } from "../site";
import type { Section, Setting } from "./types";

function collect(section: Section, into: Map<string, Section | Setting>) {
  if (section.path) into.set(section.path, section);
  for (const setting of section.settings) into.set(setting.path, setting);
  for (const child of section.sections) collect(child, into);
  return into;
}

const settings = collect(config, new Map());

export type Ref = {
  href: string;
  title: string;
  description: string;
  detail: string;
};

export function settingOf(path: string): Ref {
  const setting = settings.get(path);
  if (!setting) throw new Error(`config.json has no setting or table named ${path}`);
  return {
    href: link(`/config#${path}`),
    title: path,
    description: setting.description,
    detail: "default" in setting ? `default ${setting.default}` : "",
  };
}

export function keybindingOf(name: string) {
  const ref = settingOf(`keybindings.${name}`);
  const setting = settings.get(`keybindings.${name}`);
  const keys = setting && "default" in setting ? (JSON.parse(setting.default) as string[]) : [];
  if (keys.length === 0) throw new Error(`keybindings.${name} is not a keybinding`);
  return { ...ref, detail: "", keys };
}

const pages = api.services.flatMap((service) =>
  service.methods.map((method) => ({
    href: link(`/api/${service.slug}/${method.slug}`),
    types: new Set(referencedTypes([method.request, method.response]).messages.map(([name]) => name)),
  })),
);

export function fieldOf(message: string, name: string): Ref {
  const field = api.messages[message]?.fields.find((candidate) => candidate.name === name);
  if (!field) throw new Error(`api.json has no field ${name} on ${message}`);
  const page = pages.find((candidate) => candidate.types.has(message));
  if (!page) throw new Error(`no API page draws ${message}`);
  return {
    href: `${page.href}#${anchor(message)}-${name}`,
    title: `${message.split(".").pop() ?? message}.${name}`,
    description: field.description,
    detail: `${field.repeated ? "list of " : ""}${field.type}`,
  };
}

export function commandOf(path: string): Ref {
  const slug = path.split(" ").join("/");
  const command = commandBySlug.get(slug);
  if (!command) throw new Error(`cli.json has no command named komrad ${path}`);
  return {
    href: commandHref(slug),
    title: command.path,
    description: command.short,
    detail: command.usage,
  };
}
