#!/bin/sh
# vmstat compatibility wrapper for the embedded Android Linux runtime.
# Invokes the busybox vmstat command with the same arguments.
exec busybox vmstat "$@"