/*
 * bionic-compat.c — Android bionic 兼容 shim
 *
 * busybox 面向 glibc/Linux 编写，bionic 缺少若干 glibc 的 API。
 * 本文件为构建的 busybox 提供这些缺失函数的极简实现。
 */

#include <errno.h>
#include <fcntl.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <dirent.h>
#include <fnmatch.h>
#include <glob.h>
#include <mntent.h>
#include <sys/stat.h>
#include <utmpx.h>
#include <netinet/ether.h>
#include <net/ethernet.h>
#include <sys/timex.h>

/*
 * gethostid — bionic 无此函数。Android 没有基于 IP 的 hostid，
 * 直接返回 0 即可满足 busybox hostid applet。
 */
long gethostid(void)
{
	return 0;
}

/*
 * sigisemptyset — GNU 扩展，bionic 缺失。检查信号集是否为空集。
 */
int sigisemptyset(const sigset_t *set)
{
	const unsigned long *bits = (const unsigned long *)set;
	unsigned int i;
	unsigned int n = sizeof(sigset_t) / sizeof(unsigned long);
	for (i = 0; i < n; i++)
		if (bits[i])
			return 0;
	return 1;
}

/*
 * updwtmpx — bionic 无此函数。Android 没有传统的 wtmp 记录，
 * 容器内写 wtmp 无意义，空实现。
 */
void updwtmpx(const char *filename, const struct utmpx *utx)
{
	(void)filename;
	(void)utx;
}

/*
 * getusershell 系列 — glibc 用于枚举 /etc/shells 的接口，bionic 缺失。
 * Android 容器没有 /etc/shells，这里提供一个最小实现：/bin/sh 视为合法 shell。
 */
static const char *const daidai_shells[] = {
	"/bin/sh",
	NULL,
};

static int daidai_shell_index;

void setusershell(void)
{
	daidai_shell_index = 0;
}

void endusershell(void)
{
	daidai_shell_index = 0;
}

char *getusershell(void)
{
	const char *shell = daidai_shells[daidai_shell_index];
	if (shell == NULL)
		return NULL;
	daidai_shell_index++;
	return (char *)shell;
}

/*
 * utmpxname — glibc 用于设置 wtmp/utmp 文件名，bionic 缺失。
 * Android 没有传统 wtmp 记录，空实现即可。
 */
int utmpxname(const char *file)
{
	(void)file;
	return 0;
}

/*
 * ether_hostton — bionic 缺失。glibc 中它会把主机名解析成以太网地址，
 * 这里退化为按 MAC 字符串解析，busybox ether-wake 实际传入的就是 MAC。
 */
int ether_hostton(const char *hostname, struct ether_addr *addr)
{
	const struct ether_addr *ea = ether_aton(hostname);
	if (ea == NULL) {
		errno = EINVAL;
		return -1;
	}
	memcpy(addr, ea, sizeof(*addr));
	return 0;
}
int addmntent(FILE *fp, const struct mntent *mnt)
{
	if (fp == NULL || mnt == NULL) {
		errno = EINVAL;
		return 1;
	}
	if (fprintf(fp, "%s %s %s %s %d %d\n",
		    mnt->mnt_fsname ? mnt->mnt_fsname : "none",
		    mnt->mnt_dir ? mnt->mnt_dir : "none",
		    mnt->mnt_type ? mnt->mnt_type : "none",
		    mnt->mnt_opts ? mnt->mnt_opts : "none",
		    mnt->mnt_freq, mnt->mnt_passno) < 0)
		return 1;
	return 0;
}
