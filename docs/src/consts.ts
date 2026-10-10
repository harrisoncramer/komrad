export const SITE_TITLE = "komrad";
export const HOME_TITLE = "komrad: a terminal agent orchestrator for Claude Code and Codex";
export const SITE_DESCRIPTION =
  "An open-source tmux panel that runs, searches, schedules and coordinates Claude Code and Codex agents, on your machine or a remote box.";
export const REPO_URL = "https://github.com/harrisoncramer/komrad";
export const CANONICAL_VERSION = "latest";

export const SECTIONS = [
  { id: "guides", title: "Guides" },
  { id: "api", title: "API" },
  { id: "cli", title: "CLI" },
  { id: "config", title: "Config" },
  { id: "pricing", title: "Pricing" },
] as const;

export const SPLASH = {
  name: "splash.png",
  alt: "The komrad panel: a list of agents grouped by project beside a live Claude session.",
  width: 1920,
  height: 930,
};
