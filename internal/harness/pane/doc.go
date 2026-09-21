// Package pane says something to an agent by typing into the terminal it runs in, and learns
// what it is doing by reading what that terminal draws.
//
// All of it is a workaround. A CLI that could be handed a message directly would need none of
// this. What is here instead is the protocol for getting a line into a text box somebody else
// may be typing in: read the frame, decide whether the box may be touched at all, cut what it
// holds, paste, and hand back what was taken. The timings in it were arrived at by measuring a
// real terminal rather than by reasoning about one, so they are moved rather than edited.
//
// It sits behind harness.Channel and harness.Observer so a CLI offering something better is a
// second implementation rather than a rewrite of the delivery service. That seam is about
// messaging and state and nothing else. An agent reached over a socket would still run in a
// pane a person can attach to and take over, so launching, liveness and attaching stay where
// they are.
package pane
