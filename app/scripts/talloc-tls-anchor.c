/* Force the TLS segment alignment to 64 bytes so qemu-user can run
 * statically linked bionic test programs during waf configure checks.
 * Compiled per-ABI with -fno-emulated-tls and linked into test programs. */
_Thread_local unsigned char daidai_tls_anchor[64] __attribute__((aligned(64)));
