// Package menu holds the short lists a screen offers over the list it opened from. A menu is a
// value. Each entry picks by returning a request, and says whether the menu stays open after it.
//
// A menu may nest a menu that another part of the panel builds, such as the git menu or the jump
// menu. The root hands a menu those nested menus when it asks for them, and keeps the stack of open
// menus. The root, the git menu and the agents, documents, review, schedules and worktrees screens
// import menu. It imports only the shared packages below it, and never the root or a domain.
package menu
