// nodelauncher.c - Load libnodejs_exec.so via dlopen and call node::Start
// Compiled as PIE executable for Android arm64
// Named libnodelauncher.so so Android extracts it with exec permissions

#include <dlfcn.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>

typedef int (*NodeStartFunc)(int, char **, char **);

int main(int argc, char **argv) {
    // Load node binary as shared library
    void *handle = dlopen("libnodejs_exec.so", RTLD_NOW | RTLD_GLOBAL);
    if (!handle) {
        fprintf(stderr, "nodelauncher: Cannot load libnodejs_exec.so: %s\n", dlerror());
        return 1;
    }

    // Try node::Start(int argc, char** argv, char** exec_argv)
    // In Node.js 18, the entry point is _start or main
    // But since this is a shared lib, we need to find the actual entry
    // Node exports: node::Start(int argc, char** argv, char** exec_argv)
    NodeStartFunc node_start = (NodeStartFunc)dlsym(handle, "node_start");
    if (!node_start) {
        node_start = (NodeStartFunc)dlsym(handle, "Start");
    }
    if (!node_start) {
        // Try running as if it were an executable via execve
        // Actually, libnodejs_exec.so has a main() entry we can call
        node_start = (NodeStartFunc)dlsym(handle, "main");
    }

    if (!node_start) {
        fprintf(stderr, "nodelauncher: Cannot find Node entry point: %s\n", dlerror());
        dlclose(handle);
        return 1;
    }

    // Set up environment
    char *nodePath = getenv("NODE_PATH");
    char *nodeOptions = getenv("NODE_OPTIONS");

    // Run Node
    char **execArgv = NULL;
    int result = node_start(argc, argv, execArgv);

    dlclose(handle);
    return result;
}
