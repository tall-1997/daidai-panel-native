/*
 * bionic-compat.h — Android bionic 兼容声明
 *
 * 通过 EXTRA_CFLAGS="-include bionic-compat.h" 注入 busybox 的 C 编译单元，
 * 实现放在链接的 bionic-compat.o 中。
 * 注意：汇编预处理（.S）会定义 __ASSEMBLER__，因此这里的声明只对 C 文件生效。
 */

#ifndef DAIDAI_BIONIC_COMPAT_H
#define DAIDAI_BIONIC_COMPAT_H

#if !defined(__ASSEMBLER__)

#include <sys/types.h>
#include <unistd.h>
#include <utmpx.h>
#include <sys/timex.h>
#include <netinet/ether.h>
#include <net/ethernet.h>
#include <glob.h>
#include <signal.h>
#include <mntent.h>

long gethostid(void);
int sigisemptyset(const sigset_t *set);
int glob(const char *pattern, int flags, int (*errfunc)(const char *, int), glob_t *pglob);
void globfree(glob_t *pglob);
int addmntent(FILE *fp, const struct mntent *mnt);
int getlogin_r(char *buf, size_t bufsize);
int syncfs(int fd);
void updwtmpx(const char *filename, const struct utmpx *utx);
void setusershell(void);
void endusershell(void);
char *getusershell(void);
int adjtimex(struct timex *buf);
int utmpxname(const char *file);
ssize_t getrandom(void *buf, size_t buflen, unsigned int flags);
int ether_hostton(const char *hostname, struct ether_addr *addr);

#endif /* !__ASSEMBLER__ */
#endif /* DAIDAI_BIONIC_COMPAT_H */
