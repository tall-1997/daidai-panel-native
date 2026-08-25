#!/bin/sh
# stat compatibility wrapper for the embedded Android Linux runtime.
# Invokes the busybox stat command with the same arguments.
exec busybox stat "$@"