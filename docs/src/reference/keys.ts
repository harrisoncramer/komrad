import { config } from ".";
import type { Section } from "./types";

const PREFIX = "keybindings.";

function collect(section: Section, into: Map<string, string[]>) {
  for (const setting of section.settings) {
    if (setting.path.startsWith(PREFIX)) {
      into.set(setting.path.slice(PREFIX.length), JSON.parse(setting.default) as string[]);
    }
  }
  for (const child of section.sections) collect(child, into);
  return into;
}

export const keybindings = collect(config, new Map());

export function keysOf(name: string): string[] {
  const keys = keybindings.get(name);
  if (!keys?.length) throw new Error(`config.json has no keybinding named ${PREFIX}${name}`);
  return keys;
}
