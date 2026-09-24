import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { REPO_URL, SITE_TITLE } from "../consts";

type HeaderProps = {
  children: ReactNode;
};

const Header = ({ children }: HeaderProps) => {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    document.documentElement.toggleAttribute("data-nav-open", open);
  }, [open]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <nav className="shadow z-20 p-4 top-0 sticky flex items-center justify-between gap-4 bg-app-black text-app-white">
      <div className="flex items-center gap-4">
        <button
          aria-label="Toggle navigation"
          aria-expanded={open}
          className="md:hidden cursor-pointer"
          onClick={() => setOpen(!open)}
        >
          <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="white">
            <path d="M4 22h-4v-4h4v4zm0-12h-4v4h4v-4zm0-8h-4v4h4v-4zm3 0v4h17v-4h-17zm0 12h17v-4h-17v4zm0 8h17v-4h-17v4z" />
          </svg>
        </button>
        <a href="/" className="flex items-center gap-2 text-app-white">
          <img src="/weave.png" alt="" className="h-8" />
          <span className="font-playfair text-xl">{SITE_TITLE}</span>
        </a>
        {children}
      </div>
      <a href={REPO_URL} target="_blank" aria-label="weave on GitHub" className="w-6 shrink-0">
        <svg role="img" viewBox="0 0 128 128">
          <title>GitHub</title>
          <path
            fill="white"
            d="M64 5.103c-33.347 0-60.388 27.035-60.388 60.388 0 26.682 17.303 49.317 41.297 57.303 3.017.56 4.125-1.31 4.125-2.905 0-1.44-.056-6.197-.082-11.243-16.8 3.653-20.345-7.125-20.345-7.125-2.747-6.98-6.705-8.836-6.705-8.836-5.48-3.748.413-3.67.413-3.67 6.063.425 9.257 6.223 9.257 6.223 5.386 9.23 14.127 6.562 17.573 5.02.542-3.903 2.107-6.568 3.834-8.076-13.413-1.525-27.514-6.704-27.514-29.843 0-6.593 2.36-11.98 6.223-16.21-.628-1.52-2.695-7.662.584-15.98 0 0 5.07-1.623 16.61 6.19C53.7 35 58.867 34.327 64 34.304c5.13.023 10.3.694 15.127 2.033 11.526-7.813 16.59-6.19 16.59-6.19 3.287 8.317 1.22 14.46.593 15.98 3.872 4.23 6.215 9.617 6.215 16.21 0 23.194-14.127 28.3-27.574 29.796 2.167 1.874 4.097 5.55 4.097 11.183 0 8.08-.07 14.583-.07 16.572 0 1.607 1.088 3.49 4.148 2.897 23.98-7.994 41.263-30.622 41.263-57.294C124.388 32.14 97.35 5.104 64 5.104z"
          />
        </svg>
      </a>
    </nav>
  );
};

export default Header;
