// Package schedules holds the screen that lists the schedules, and the archive that lists the
// archived ones. The live and the archived schedules are two kinds of the same view. The block
// beside the list shows the schedule's document with its runs.
//
// The package owns the form that creates and edits a schedule, the name typed over a row, and the
// menus that archive, restore and delete a schedule for good.
//
// The root is the host. It opens the screen, renders its list from the parameters the screen hands
// it, and applies the writes it asks for. The schedules screen reaches the host only through the
// requests it returns, and it never imports the root or another domain.
package schedules
