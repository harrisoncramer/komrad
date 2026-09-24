import { useEffect, useRef, useState } from "react";
import { isCurrent } from "./nav";
import type { NavLink } from "./nav";

type NavigationDropdownProps = {
  links: readonly NavLink[];
  path: string;
};

const MENU_ID = "mobile-navigation";

const NavigationDropdown = ({ links, path }: NavigationDropdownProps) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const [isOpen, setIsOpen] = useState(false);

  useEffect(() => {
    if (!isOpen) return;

    const onPointerDown = (event: PointerEvent) => {
      if (event.target instanceof Node && containerRef.current?.contains(event.target)) return;
      setIsOpen(false);
    };

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      setIsOpen(false);
      buttonRef.current?.focus();
    };

    const desktop = window.matchMedia("(min-width: 768px)");
    const onBreakpoint = (event: MediaQueryListEvent) => {
      if (event.matches) setIsOpen(false);
    };

    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    desktop.addEventListener("change", onBreakpoint);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
      desktop.removeEventListener("change", onBreakpoint);
    };
  }, [isOpen]);

  return (
    <div ref={containerRef}>
      <button
        ref={buttonRef}
        type="button"
        className="relative flex h-10 w-10 items-center justify-center rounded-xs text-app-white outline-hidden hover:bg-app-background-light focus-visible:bg-app-background-light"
        aria-label="Menu"
        aria-expanded={isOpen}
        aria-controls={MENU_ID}
        onClick={() => setIsOpen((open) => !open)}
      >
        <span
          aria-hidden="true"
          className={`absolute h-0.5 w-5 bg-app-white transition-[rotate,translate] duration-200 ease-out-expo ${isOpen ? "rotate-45" : "-translate-y-1.5"}`}
        />
        <span
          aria-hidden="true"
          className={`absolute h-0.5 w-5 bg-app-white transition-opacity duration-150 ${isOpen ? "opacity-0" : "opacity-100"}`}
        />
        <span
          aria-hidden="true"
          className={`absolute h-0.5 w-5 bg-app-white transition-[rotate,translate] duration-200 ease-out-expo ${isOpen ? "-rotate-45" : "translate-y-1.5"}`}
        />
      </button>
      <ul
        id={MENU_ID}
        className={`absolute right-0 top-full min-w-[180px] rounded-b-md border border-app-background-medium bg-app-background-dark pb-3 pt-2 shadow-3xl ${isOpen ? "visible [clip-path:inset(0_0_-1rem_0)] [transition:clip-path_220ms_cubic-bezier(0.16,1,0.3,1),visibility_0s]" : "invisible [clip-path:inset(0_0_100%_0)] [transition:clip-path_160ms_cubic-bezier(0.7,0,0.84,0),visibility_0s_160ms]"}`}
      >
        {links.map((link) => (
          <li key={link.href}>
            <a
              href={link.href}
              aria-current={isCurrent(link.href, path) ? "page" : undefined}
              className="block px-4 py-2.5 font-['Playfair_Display'] text-base text-app-white hover:bg-app-background-light aria-[current=page]:text-app-blue"
            >
              {link.label}
            </a>
          </li>
        ))}
      </ul>
    </div>
  );
};

export default NavigationDropdown;
