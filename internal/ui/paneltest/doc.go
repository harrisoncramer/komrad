// Package paneltest drives the panel the way a person, komrad and the terminal do, and reads back
// what they would see. It runs the panel over a recorded tmux and a server that records each call
// and answers as a test tells it to. It sends watch events, presses keys, and reads frames and rows
// back.
//
// The package also compares a frame against a golden file and holds the letters of the pane picker.
//
// The root's stories in package ui_test and each domain's stories in its own test package import
// paneltest. It imports the root, so the root never imports it.
package paneltest
