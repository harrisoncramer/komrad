// Package transcript stores each agent's transcript as turns of searchable text. The transcript
// sync appends turns as it reads them from the CLI's own log, and the package searches those
// turns and reads an agent's last turns back.
package transcript
