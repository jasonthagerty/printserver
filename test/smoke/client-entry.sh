#!/bin/sh
# Starts an mDNS stack, then runs the given command.
set -e
mkdir -p /run/dbus
dbus-daemon --system --fork --nopidfile
avahi-daemon --no-chroot --no-drop-root -D
sleep 3
exec "$@"
