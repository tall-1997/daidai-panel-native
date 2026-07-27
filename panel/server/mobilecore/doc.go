// Package mobilecore provides the embedded Android management Core lifecycle.
//
// The Android host must run this package in a dedicated process declared as
// android:process=":panel". Process-global Go state belongs exclusively to the
// Core in that process and is restored only for deterministic lifecycle reuse.
package mobilecore
