#include <Python.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static void print_status(PyStatus status) {
    if (status.err_msg) {
        fprintf(stderr, "%s\n", status.err_msg);
    } else {
        fprintf(stderr, "Python initialization failed\n");
    }
}

int main(int argc, char **argv) {
    if (argc < 3) {
        fprintf(stderr, "usage: libpython_exec.so <python-home> <script|-m> [args...]\n");
        return 2;
    }

    const char *home = argv[1];
    int py_argc = argc - 2;
    char **py_argv = &argv[2];

    PyStatus status;
    PyConfig config;
    PyConfig_InitPythonConfig(&config);

    status = PyConfig_SetBytesArgv(&config, py_argc, py_argv);
    if (PyStatus_Exception(status)) {
        print_status(status);
        PyConfig_Clear(&config);
        return 1;
    }

    status = PyConfig_SetBytesString(&config, &config.home, home);
    if (PyStatus_Exception(status)) {
        print_status(status);
        PyConfig_Clear(&config);
        return 1;
    }

    status = Py_InitializeFromConfig(&config);
    if (PyStatus_Exception(status)) {
        print_status(status);
        PyConfig_Clear(&config);
        return 1;
    }

    PyConfig_Clear(&config);
    return Py_RunMain();
}
