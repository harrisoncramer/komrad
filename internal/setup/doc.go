// Package setup takes a machine from a fresh install toward a passing doctor, one step at a time.
// The steps cover the config file, tmux, the database, the default harness, the standing context
// and a forge connection.
//
// A step never keeps a record of being done. It asks the doctor, repairs what the doctor found,
// and asks again, so running setup twice finds the finished steps already passing.
package setup
