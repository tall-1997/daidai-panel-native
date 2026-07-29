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
    int py_argc = argc - 1;
    char **py_argv = calloc((size_t)py_argc + 1, sizeof(char*));
    if (!py_argv) {
        fprintf(stderr, "failed to allocate argv\n");
        return 1;
    }
    py_argv[0] = "python";
    for (int i = 2; i < argc; i++) {
        py_argv[i - 1] = argv[i];
    }

    PyStatus status;
    PyConfig config;
    PyConfig_InitPythonConfig(&config);

    status = PyConfig_SetBytesArgv(&config, py_argc, py_argv);
    if (PyStatus_Exception(status)) {
        print_status(status);
        PyConfig_Clear(&config);
        free(py_argv);
        return 1;
    }

    status = PyConfig_SetBytesString(&config, &config.home, home);
    if (PyStatus_Exception(status)) {
        print_status(status);
        PyConfig_Clear(&config);
        free(py_argv);
        return 1;
    }

    status = Py_InitializeFromConfig(&config);
    if (PyStatus_Exception(status)) {
        print_status(status);
        PyConfig_Clear(&config);
        free(py_argv);
        return 1;
    }

    PyRun_SimpleString(
        "import os, sys\n"
        "sys.stdout = os.fdopen(1, 'w', buffering=1, closefd=False)\n"
        "sys.stderr = os.fdopen(2, 'w', buffering=1, closefd=False)\n"
    );

    PyConfig_Clear(&config);
    int result = Py_RunMain();
    free(py_argv);
    return result;
}
