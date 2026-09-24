import { useEffect, useRef, useState } from "react";
import { REPO_URL } from "../consts";
import { BUILD } from "../site";

type Release = { version: string; commit: string };

type Versions = { stable: Release[]; unstable: Release[] };

const short = (commit: string) => commit.slice(0, 7);

const showsCommit = (version: string) => version === "tip" || version === "dev";

const VersionPicker = () => {
  const containerRef = useRef<HTMLDivElement>(null);
  const [isOpen, setIsOpen] = useState(false);
  const [versions, setVersions] = useState<Versions | null>(null);

  useEffect(() => {
    if (BUILD.version === "dev") return;
    fetch("/versions.json")
      .then((response) => (response.ok ? response.json() : null))
      .then((body: Versions | null) => {
        setVersions(body);
      })
      .catch(() => {
        setVersions(null);
      });
  }, []);

  useEffect(() => {
    if (!isOpen) return;

    const onPointerDown = (event: PointerEvent) => {
      if (event.target instanceof Node && containerRef.current?.contains(event.target)) return;
      setIsOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setIsOpen(false);
    };

    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [isOpen]);

  const hrefFor = (version: string) => {
    const base = import.meta.env.BASE_URL.replace(/\/$/, "");
    const rest = window.location.pathname.slice(base.length) || "/";
    return `/${version}${rest}${window.location.hash}`;
  };

  const groups = versions
    ? [
        { title: "Stable", releases: versions.stable },
        { title: "Unstable", releases: versions.unstable },
      ].filter((group) => group.releases.length > 0)
    : [];

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        className="flex h-7 shrink-0 cursor-pointer items-center gap-1 rounded-md border border-app-background-medium px-1.5 font-mono text-[11px] whitespace-nowrap text-app-gray-light transition-colors hover:border-app-blue/40 hover:text-app-white md:h-9 md:gap-1.5 md:px-2.5 md:text-xs"
        aria-label="Documentation version"
        aria-expanded={isOpen}
        onClick={() => {
          setIsOpen((open) => !open);
        }}
      >
        {BUILD.version}
        {showsCommit(BUILD.version) && BUILD.commit && (
          <span className="hidden md:inline">· {short(BUILD.commit)}</span>
        )}
        <svg aria-hidden="true" viewBox="0 0 16 16" className="hidden h-3 w-3 md:block" fill="currentColor">
          <path d="M4.2 6.2a.75.75 0 0 1 1.06 0L8 8.94l2.74-2.74a.75.75 0 1 1 1.06 1.06L8.53 10.53a.75.75 0 0 1-1.06 0L4.2 7.26a.75.75 0 0 1 0-1.06Z" />
        </svg>
      </button>
      {isOpen && (
        <div className="absolute top-full right-0 z-30 mt-2 w-60 overflow-hidden rounded-lg border border-app-background-medium bg-app-background-dark shadow-3xl">
          {groups.map((group) => (
            <div key={group.title} className="border-b border-app-background-medium py-1 last:border-b-0">
              <p className="px-3 pt-2 pb-1 text-xs tracking-wide text-app-gray-dim uppercase">{group.title}</p>
              {group.releases.map((release, index) => (
                <a
                  key={release.version}
                  href={hrefFor(release.version)}
                  aria-current={release.version === BUILD.version ? "page" : undefined}
                  className="flex items-center justify-between px-3 py-1.5 text-sm text-app-white hover:bg-app-background-light aria-[current=page]:text-app-blue"
                >
                  <span className="font-mono">{release.version}</span>
                  <span className="font-mono text-xs text-app-gray-dim">
                    {group.title === "Stable" && index === 0 ? "latest" : short(release.commit)}
                  </span>
                </a>
              ))}
            </div>
          ))}
          <div className="px-3 py-2 text-xs text-app-gray-dim">
            {groups.length === 0 && <p className="pb-1">No other versions published.</p>}
            {BUILD.commit ? (
              <a
                href={`${REPO_URL}/tree/${BUILD.commit}`}
                className="font-mono hover:text-app-blue"
                target="_blank"
                rel="noreferrer"
              >
                Built from {short(BUILD.commit)}
              </a>
            ) : (
              <span>Built from a working tree</span>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default VersionPicker;
