export type NavLink = { href: string; label: string };

export function isCurrent(href: string, path: string): boolean {
  return path === href || path.startsWith(`${href}/`);
}
