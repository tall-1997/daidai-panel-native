#include <jni.h>
#include <errno.h>
#include <pty.h>
#include <signal.h>
#include <stdio.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/wait.h>
#include <unistd.h>

static void throw_io(JNIEnv *env, const char *operation) {
    jclass type = (*env)->FindClass(env, "java/io/IOException");
    char message[256];
    snprintf(message, sizeof(message), "%s: %s", operation, strerror(errno));
    (*env)->ThrowNew(env, type, message);
}

static char **copy_strings(JNIEnv *env, jobjectArray values) {
    jsize count = values == NULL ? 0 : (*env)->GetArrayLength(env, values);
    char **result = calloc((size_t)count + 1, sizeof(char *));
    if (result == NULL) return NULL;
    for (jsize index = 0; index < count; index++) {
        jstring value = (jstring)(*env)->GetObjectArrayElement(env, values, index);
        const char *utf = (*env)->GetStringUTFChars(env, value, NULL);
        result[index] = strdup(utf);
        (*env)->ReleaseStringUTFChars(env, value, utf);
        (*env)->DeleteLocalRef(env, value);
        if (result[index] == NULL) return result;
    }
    return result;
}

static void free_strings(char **values) {
    if (values == NULL) return;
    for (size_t index = 0; values[index] != NULL; index++) free(values[index]);
    free(values);
}

JNIEXPORT jlong JNICALL
Java_com_daidai_daidai_1app_AndroidPtyBridge_nativeStart(
        JNIEnv *env, jobject unused, jobjectArray command, jobjectArray environment,
        jstring working_directory, jint rows, jint columns) {
    (void)unused;
    char **argv = copy_strings(env, command);
    char **env_values = copy_strings(env, environment);
    const char *cwd_utf = (*env)->GetStringUTFChars(env, working_directory, NULL);
    char *cwd = cwd_utf == NULL ? NULL : strdup(cwd_utf);
    if (cwd_utf != NULL) (*env)->ReleaseStringUTFChars(env, working_directory, cwd_utf);
    if (argv == NULL || argv[0] == NULL || env_values == NULL || cwd == NULL) {
        free_strings(argv);
        free_strings(env_values);
        free(cwd);
        errno = ENOMEM;
        throw_io(env, "allocate PTY arguments");
        return -1;
    }
    struct winsize size = {.ws_row = (unsigned short)rows, .ws_col = (unsigned short)columns};
    int master = -1;
    pid_t pid = forkpty(&master, NULL, NULL, &size);
    if (pid < 0) {
        free_strings(argv);
        free_strings(env_values);
        free(cwd);
        throw_io(env, "forkpty");
        return -1;
    }
    if (pid == 0) {
        chdir(cwd);
        execve(argv[0], argv, env_values);
        _exit(127);
    }
    free_strings(argv);
    free_strings(env_values);
    free(cwd);
    return ((jlong)(uint32_t)pid << 32) | (uint32_t)master;
}

JNIEXPORT jint JNICALL
Java_com_daidai_daidai_1app_AndroidPtyBridge_nativeRead(
        JNIEnv *env, jobject unused, jint fd, jbyteArray buffer) {
    (void)unused;
    jsize capacity = (*env)->GetArrayLength(env, buffer);
    jbyte *bytes = (*env)->GetByteArrayElements(env, buffer, NULL);
    ssize_t count;
    do count = read(fd, bytes, (size_t)capacity); while (count < 0 && errno == EINTR);
    if (count > 0) (*env)->ReleaseByteArrayElements(env, buffer, bytes, 0);
    else (*env)->ReleaseByteArrayElements(env, buffer, bytes, JNI_ABORT);
    if (count < 0 && errno != EIO && errno != EBADF) throw_io(env, "read PTY");
    return count < 0 ? -1 : (jint)count;
}

JNIEXPORT void JNICALL
Java_com_daidai_daidai_1app_AndroidPtyBridge_nativeWrite(
        JNIEnv *env, jobject unused, jint fd, jbyteArray data) {
    (void)unused;
    jsize length = (*env)->GetArrayLength(env, data);
    jbyte *bytes = (*env)->GetByteArrayElements(env, data, NULL);
    size_t offset = 0;
    while (offset < (size_t)length) {
        ssize_t count = write(fd, bytes + offset, (size_t)length - offset);
        if (count > 0) offset += (size_t)count;
        else if (count < 0 && errno == EINTR) continue;
        else { throw_io(env, "write PTY"); break; }
    }
    (*env)->ReleaseByteArrayElements(env, data, bytes, JNI_ABORT);
}

JNIEXPORT void JNICALL
Java_com_daidai_daidai_1app_AndroidPtyBridge_nativeResize(
        JNIEnv *env, jobject unused, jint fd, jint rows, jint columns) {
    (void)unused;
    struct winsize size = {.ws_row = (unsigned short)rows, .ws_col = (unsigned short)columns};
    if (ioctl(fd, TIOCSWINSZ, &size) != 0) throw_io(env, "resize PTY");
}

JNIEXPORT jint JNICALL
Java_com_daidai_daidai_1app_AndroidPtyBridge_nativeStop(
        JNIEnv *env, jobject unused, jint fd, jint pid) {
    (void)env;
    (void)unused;
    if (fd >= 0) close(fd);
    if (pid > 0) {
        kill(-pid, SIGTERM);
        kill(pid, SIGTERM);
        int status = 0;
        for (int attempt = 0; attempt < 20; attempt++) {
            pid_t result = waitpid(pid, &status, WNOHANG);
            if (result == pid) return WIFEXITED(status) ? WEXITSTATUS(status) : 128 + WTERMSIG(status);
            usleep(10000);
        }
        kill(-pid, SIGKILL);
        kill(pid, SIGKILL);
        waitpid(pid, &status, 0);
        return WIFEXITED(status) ? WEXITSTATUS(status) : 128 + WTERMSIG(status);
    }
    return 0;
}
