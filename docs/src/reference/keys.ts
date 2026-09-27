import { config } from ".";
import type { Section, Setting } from "./types";

const PREFIX = "keybindings.";

function collect(section: Section, into: Map<string, Setting>) {
  for (const setting of section.settings) {
    if (setting.path.startsWith(PREFIX)) into.set(setting.path.slice(PREFIX.length), setting);
  }
  for (const child of section.sections) collect(child, into);
  return into;
}

export const keybindings = collect(config, new Map());

export function bindingOf(name: string) {
  const setting = keybindings.get(name);
  const keys = setting ? (JSON.parse(setting.default) as string[]) : [];
  if (!setting || keys.length === 0) throw new Error(`config.json has no keybinding named ${PREFIX}${name}`);
  return { path: setting.path, description: setting.description, keys };
}
