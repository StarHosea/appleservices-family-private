#define _GNU_SOURCE
#include <arpa/inet.h>
#include <dlfcn.h>
#include <errno.h>
#include <sys/socket.h>

/* Confine only the helper's unused Rust APS transport. The Go worker owns the
 * real courier connection. No firewall or other process is changed. */
int connect(int fd, const struct sockaddr *address, socklen_t size) {
    if (address && ((address->sa_family == AF_INET && size >= sizeof(struct sockaddr_in)
                    && ntohs(((const struct sockaddr_in *)address)->sin_port) == 5223)
                || (address->sa_family == AF_INET6 && size >= sizeof(struct sockaddr_in6)
                    && ntohs(((const struct sockaddr_in6 *)address)->sin6_port) == 5223))) {
        errno = ECONNREFUSED;
        return -1;
    }
    int (*original)(int, const struct sockaddr *, socklen_t) = dlsym(RTLD_NEXT, "connect");
    if (!original) { errno = ENOSYS; return -1; }
    return original(fd, address, size);
}
