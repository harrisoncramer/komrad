import { anchor, api, commandBySlug, commandHref, config, escape, inline, referencedTypes, typeName } from ".";
import { link } from "../site";
import type { Field, Section, Setting } from "./types";

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

const fieldRef = (message: string, field: Field, href: string): Ref => ({
  href,
  title: `${message.split(".").pop() ?? message}.${field.name}`,
  description: field.description,
  detail: typeName(field),
});

export function fieldOf(message: string, name: string): Ref {
  const field = api.messages[message]?.fields.find((candidate) => candidate.name === name);
  if (!field) throw new Error(`api.json has no field ${name} on ${message}`);
  const page = pages.find((candidate) => candidate.types.has(message));
  if (!page) throw new Error(`no API page draws ${message}`);
  return fieldRef(message, field, `${page.href}#${anchor(message)}-${name}`);
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

export const refLink = (target: Ref, inner: string) =>
  `<a class="ref" href="${escape(target.href)}">${inner}<span class="ref-card" aria-hidden="true">` +
  `<code>${escape(target.title)}</code>` +
  (target.description ? `<span>${inline(target.description)}</span>` : "") +
  (target.detail ? `<span class="ref-detail">${escape(target.detail)}</span>` : "") +
  `</span></a>`;

type Scope = {
  messages?: string[];
  enum?: string;
};

const methods = new Map(
  api.services.flatMap((service) =>
    service.methods.map((method): [string, Ref] => [
      method.tool,
      {
        href: link(`/api/${service.slug}/${method.slug}`),
        title: method.tool,
        description: method.summary,
        detail: method.path,
      },
    ]),
  ),
);

const hookMoment = /^(agent|task)\.(pre|post)-[a-z]+$/;

function codeRef({ messages = [], enum: enumName }: Scope, code: string): Ref | undefined {
  for (const message of messages) {
    const field = api.messages[message]?.fields.find((candidate) => candidate.name === code);
    if (field) return fieldRef(message, field, `#${anchor(message)}-${code}`);
  }

  if (enumName) {
    const value = api.enums[enumName]?.values.find(({ name }) => name === code || name.endsWith(`_${code}`));
    if (value) {
      return {
        href: `#${anchor(enumName)}-${value.name}`,
        title: value.name,
        description: value.description,
        detail: "",
      };
    }
  }

  const method = methods.get(code);
  if (method) return method;

  const event = api.events.find((candidate) => candidate.name === code);
  if (event) return { href: link(`/api#event-${code}`), title: code, description: event.description, detail: "" };

  if (code.includes(".") && settings.has(code)) return settingOf(code);

  const lifecycle = hookMoment.exec(code)?.[1];
  if (lifecycle)
    return { href: link(`/guides/hooks#${lifecycle}-lifecycle`), title: code, description: "", detail: "" };

  return undefined;
}

export const codeLink = (scope: Scope) => (code: string) => {
  const target = codeRef(scope, code);
  return target && refLink(target, `<code>${code}</code>`);
};
