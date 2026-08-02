// pylauncher.c - Load libpython3.14.so via dlopen and call Py_Main
// Compiled as PIE executable for Android arm64

#include <dlfcn.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <wchar.h>

typedef int (*Py_MainFunc)(int, wchar_t **);
typedef void (*Py_SetPythonHomeFunc)(const wchar_t *);
typedef void (*Py_SetPathFunc)(const wchar_t *);

int main(int argc, char **argv) {
    // Load libpython3.14.so
    void *handle = dlopen("libpython3.14.so", RTLD_NOW | RTLD_GLOBAL);
    if (!handle) {
        fprintf(stderr, "pylauncher: Cannot load libpython3.14.so: %s\n", dlerror());
        return 1;
    }

    // Convert env vars to wchar_t
    char *home = getenv("PYTHONHOME");
    char *pypath = getenv("PYTHONPATH");
    
    // Set Python home
    Py_SetPythonHomeFunc set_home = (Py_SetPythonHomeFunc)dlsym(handle, "Py_SetPythonHome");
    if (set_home && home) {
        size_t len = strlen(home);
        wchar_t *whome = (wchar_t *)malloc((len + 1) * sizeof(wchar_t));
        mbstowcs(whome, home, len + 1);
        set_home(whome);
        free(whome);
    }
    
    // Set Python path
    Py_SetPathFunc set_path = (Py_SetPathFunc)dlsym(handle, "Py_SetPath");
    if (set_path && pypath) {
        size_t len = strlen(pypath);
        wchar_t *wpath = (wchar_t *)malloc((len + 1) * sizeof(wchar_t));
        mbstowcs(wpath, pypath, len + 1);
        set_path(wpath);
        free(wpath);
    }

    // Convert argv to wchar_t
    wchar_t **wargv = (wchar_t **)malloc(argc * sizeof(wchar_t *));
    for (int i = 0; i < argc; i++) {
        size_t len = strlen(argv[i]);
        wargv[i] = (wchar_t *)malloc((len + 1) * sizeof(wchar_t));
        mbstowcs(wargv[i], argv[i], len + 1);
    }

    // Get Py_Main (Python 3.14 uses wchar_t argv)
    Py_MainFunc py_main = (Py_MainFunc)dlsym(handle, "Py_Main");
    if (!py_main) {
        fprintf(stderr, "pylauncher: Cannot find Py_Main: %s\n", dlerror());
        dlclose(handle);
        return 1;
    }

    // Run Python
    int result = py_main(argc, wargv);

    // Cleanup
    for (int i = 0; i < argc; i++) free(wargv[i]);
    free(wargv);
    dlclose(handle);
    return result;
}
