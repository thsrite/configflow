package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Isolation failures: the comparison must run without any route out.
var (
	errIsolation   = errors.New("network isolation missing")
	errHostSocket  = errors.New("host IPC socket visible in namespace")
	errDNSEscape   = errors.New("DNS resolution escapes the namespace")
	errEgressProbe = errors.New("unexpected egress probe result")
	errEgress      = errors.New("egress connection succeeded")
)

// assertNoEgress proves the namespace has no route out: interfaces, host IPC
// resolvers, DNS and TCP.
func assertNoEgress() error {
	interfaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("list interfaces: %w", err)
	}
	if len(interfaces) != 1 || interfaces[0].Name != "lo" {
		return errIsolation
	}
	// Filesystem sockets are not network-namespaced: systemd-resolved, nscd,
	// D-Bus and Docker would all carry requests out of an otherwise empty netns.
	for _, path := range []string{"/run/systemd/resolve/io.systemd.Resolve", "/run/nscd/socket",
		"/run/dbus/system_bus_socket", "/run/docker.sock", "/var/run/docker.sock"} {
		if _, err := os.Stat(path); err == nil {
			return errHostSocket
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := net.DefaultResolver.LookupHost(ctx, "example.com"); err == nil {
		return errDNSEscape
	}
	for _, address := range []string{"192.0.2.1:80", "[2001:db8::1]:80"} {
		conn, err := net.DialTimeout("tcp", address, 2*time.Second)
		if err == nil {
			conn.Close()
			return errEgress
		}
		if !errors.Is(err, syscall.ENETUNREACH) && !errors.Is(err, syscall.EHOSTUNREACH) &&
			!errors.Is(err, syscall.EADDRNOTAVAIL) {
			return errEgressProbe
		}
	}
	return nil
}

// readOnlyMount reports whether path lives on a read-only mount.
func readOnlyMount(path string) (bool, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return false, fmt.Errorf("stat snapshot mount: %w", err)
	}
	return st.Flags&unix.ST_RDONLY != 0, nil
}
