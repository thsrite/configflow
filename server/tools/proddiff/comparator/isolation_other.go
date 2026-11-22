//go:build !linux

package main

import "errors"

var errUnsupported = errors.New("proddiff comparator runs only on Linux test servers")

func assertNoEgress() error { return errUnsupported }

func readOnlyMount(string) (bool, error) { return false, errUnsupported }
