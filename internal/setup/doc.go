// Package setup takes a machine from a fresh install toward a green doctor, one step at a time.
//
// A step never decides for itself whether the machine is ready. It asks the doctor, repairs
// what the doctor found, and asks again, so running setup twice finds the finished steps
// already done rather than reading a record of them.
package setup
