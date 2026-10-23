// Package harness is the boundary between komrad and the CLI an agent runs, such as Claude Code
// or Codex.
//
// A [Harness] knows the command line that starts or resumes a conversation, where the CLI writes
// its conversation log, and how to read that log into turns. It knows how to read a captured frame
// for whether the agent is working or waiting, and how to read the CLI hook payloads. It sends the
// CLI a line of text over a [Channel]. The registry holds the harnesses this build has and returns
// one by the id an agent or workflow names.
package harness
