import type { MarkdownHeading } from "astro";
import { GUIDE_GROUPS, getGuides } from "./guides";
import { api, commandBySlug, commandHref, commands } from "./reference";
import { link } from "./site";

export type SidebarLink = {
  label: string;
  href: string;
  current?: boolean;
  section?: string;
};

export type SidebarItem = SidebarLink & {
  children?: SidebarLink[];
};

export type SidebarGroup = {
  title: string;
  items: SidebarItem[];
};

const section = (id: string, label: string): SidebarLink => ({ label, href: `#${id}`, section: id });

export const guideSidebar = async (current: string, headings: MarkdownHeading[]): Promise<SidebarGroup[]> => {
  const guides = await getGuides();
  const sections = headings
    .filter((heading) => heading.depth === 2)
    .map((heading) => section(heading.slug, heading.text));
  return GUIDE_GROUPS.map((group) => ({
    title: group.title,
    items: guides
      .filter((guide) => guide.data.group === group.id)
      .map((guide) => ({
        label: guide.data.title,
        href: link(`/guides/${guide.id}`),
        current: guide.id === current,
        children: guide.id === current ? sections : undefined,
      })),
  }));
};

export const API_SECTIONS = [
  { id: "requests", label: "Requests" },
  { id: "responses", label: "Responses" },
  { id: "pagination", label: "Pagination" },
  { id: "errors", label: "Errors" },
] as const;

export const apiSidebar = (current: { service?: string; method?: string } = {}): SidebarGroup[] => {
  const overview = current.service === undefined;
  return [
    {
      title: "Overview",
      items: [
        {
          label: "Calling the API",
          href: link("/api"),
          current: overview,
          children: overview ? API_SECTIONS.map(({ id, label }) => section(id, label)) : undefined,
        },
      ],
    },
    {
      title: "Services",
      items: api.services.map((service) => ({
        label: service.title,
        href: link(`/api/${service.slug}`),
        current: service.slug === current.service && current.method === undefined,
        children:
          service.slug === current.service
            ? service.methods.map((method) => ({
                label: method.tool,
                href: link(`/api/${service.slug}/${method.slug}`),
                current: method.slug === current.method,
              }))
            : undefined,
      })),
    },
  ];
};

export const cliSidebar = (current: string): SidebarGroup[] => {
  const open = commandBySlug.get(current)?.parent || current;
  const entry = (slug: string): SidebarItem => ({
    label: slug === "" ? "komrad" : slug.replaceAll("/", " "),
    href: commandHref(slug),
    current: slug === current,
  });
  return [
    {
      title: "Commands",
      items: commands
        .filter((command) => command.parent === "")
        .map((command) => ({
          ...entry(command.slug),
          children: command.slug !== "" && command.slug === open ? command.children.map(entry) : undefined,
        })),
    },
  ];
};

export const configSidebar = (tables: { id: string; label: string }[]): SidebarGroup[] => [
  { title: "Tables", items: tables.map(({ id, label }) => section(id, label)) },
];
