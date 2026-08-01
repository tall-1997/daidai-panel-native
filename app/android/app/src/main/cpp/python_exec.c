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
    if (argc < 2) {
        fprintf(stderr, "usage: libpython_exec.so <python-home> <script|-m> [args...]\n");
        return 2;
    }

    const char *home = NULL;
    int python_arg_offset = 1;
    if (argv[1][0] != '-') {
        home = argv[1];
        python_arg_offset = 2;
    } else {
        home = getenv("PYTHONHOME");
    }
    if (!home || !home[0] || argc <= python_arg_offset) {
        fprintf(stderr, "PYTHONHOME or an explicit python-home argument is required\n");
        return 2;
    }
    size_t ca_path_size = strlen(home) + sizeof("/etc/ssl/certs/cacert.pem");
    char *ca_path = malloc(ca_path_size);
    if (!ca_path) {
        fprintf(stderr, "failed to allocate CA path\n");
        return 1;
    }
    snprintf(ca_path, ca_path_size, "%s/etc/ssl/certs/cacert.pem", home);
    if (setenv("PYTHONHOME", home, 1) != 0) {
        perror("failed to set PYTHONHOME");
        free(ca_path);
        return 1;
    }
    setenv("PYTHONNOUSERSITE", "1", 0);
    setenv("PIP_ONLY_BINARY", ":all:", 0);
    setenv("PIP_PREFER_BINARY", "1", 0);
    setenv("PIP_DISABLE_PIP_VERSION_CHECK", "1", 0);
    setenv("SSL_CERT_FILE", ca_path, 0);
    setenv("REQUESTS_CA_BUNDLE", ca_path, 0);
    free(ca_path);
    int py_argc = argc - python_arg_offset + 1;
    char **py_argv = calloc((size_t)py_argc + 1, sizeof(char*));
    if (!py_argv) {
        fprintf(stderr, "failed to allocate argv\n");
        return 1;
    }
    py_argv[0] = "python";
    for (int i = python_arg_offset; i < argc; i++) {
        py_argv[i - python_arg_offset + 1] = argv[i];
    }

    PyStatus status;
    PyConfig config;
    PyConfig_InitPythonConfig(&config);
    config.use_environment = 1;
    config.user_site_directory = 0;

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
