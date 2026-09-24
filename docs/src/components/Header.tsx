import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import NavigationDropdown from "./NavigationDropdown";
import { REPO_URL, SECTIONS, SITE_TITLE } from "../consts";
import { isCurrent } from "./nav";
import type { NavLink } from "./nav";

type HeaderProps = {
  children: ReactNode;
  path: string;
};

const NAV_LINKS: readonly NavLink[] = SECTIONS.map((section) => ({
  href: `/${section.id}`,
  label: section.title,
}));

const HIDE_AFTER_PX = 400;

function normalizePath(path: string): string {
  return path.replace(/\/+$/, "") || "/";
}

const Header = ({ children, path }: HeaderProps) => {
  const headerRef = useRef<HTMLElement>(null);
  const [isHidden, setIsHidden] = useState(false);
  const currentPath = normalizePath(path);

  useEffect(() => {
    let lastOffset = window.scrollY;

    const onScroll = () => {
      const offset = window.scrollY;
      const isScrollingDown = offset > lastOffset;
      lastOffset = offset;

      if (headerRef.current?.contains(document.activeElement)) {
        setIsHidden(false);
        return;
      }

      setIsHidden(isScrollingDown && offset > HIDE_AFTER_PX);
    };

    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  return (
    <header
      ref={headerRef}
      className={`sticky top-0 z-10 border-b border-app-background-medium bg-app-black/90 text-app-white shadow-sm backdrop-blur transition-[translate,visibility] duration-300 ${isHidden ? "invisible -translate-y-full" : "visible translate-y-0"}`}
    >
      <div className="grid grid-cols-[1fr_auto_auto] items-center gap-x-2 px-4 py-3 md:grid-cols-[1fr_auto_1fr] md:gap-x-8">
        <a
          href="/"
          data-site-title
          className="col-start-1 row-start-1 flex items-center gap-2 justify-self-start font-['Playfair_Display'] text-base text-app-white transition-opacity duration-200"
        >
          <img src="/favicon.png" alt="" className="h-7 w-7" />
          {SITE_TITLE}
        </a>

        <nav aria-label="Main" className="hidden md:col-start-2 md:row-start-1 md:block">
          <ul className="flex items-center gap-8">
            {NAV_LINKS.map((link) => (
              <li key={link.href}>
                <a
                  href={link.href}
                  aria-current={isCurrent(link.href, currentPath) ? "page" : undefined}
                  className="block pt-2 font-['Playfair_Display'] text-base text-app-white after:mx-auto after:mt-[5px] after:block after:h-[3px] after:w-0 after:bg-transparent after:transition-[width,background-color] after:duration-300 after:ease-in-out hover:after:w-4/5 hover:after:bg-app-blue focus-visible:after:w-4/5 focus-visible:after:bg-app-blue aria-[current=page]:after:w-4/5 aria-[current=page]:after:bg-app-blue"
                >
                  {link.label}
                </a>
              </li>
            ))}
          </ul>
        </nav>

        <nav aria-label="Main" className="col-start-3 row-start-1 md:hidden">
          <NavigationDropdown links={NAV_LINKS} path={currentPath} />
        </nav>

        <div className="col-start-2 row-start-1 flex items-center gap-1 justify-self-end md:col-start-3 md:gap-3">
          {children}
          <a
            href={REPO_URL}
            target="_blank"
            rel="noreferrer"
            aria-label="weave on GitHub"
            className="flex h-10 w-10 items-center justify-center rounded-xs text-app-gray transition-colors duration-200 hover:bg-app-background-light hover:text-app-white focus-visible:bg-app-background-light focus-visible:text-app-white"
          >
            <svg aria-hidden="true" viewBox="0 0 16 16" fill="currentColor" className="h-5 w-5">
              <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z" />
            </svg>
          </a>
        </div>
      </div>
    </header>
  );
};

export default Header;
