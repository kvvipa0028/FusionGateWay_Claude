/* Synthetic native boundary probe. Never queries or prints Keychain items. */
#include <servers/bootstrap.h>
#include <mach/mach.h>
#include <sys/socket.h>
#include <sys/wait.h>
#include <arpa/inet.h>
#include <unistd.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <errno.h>

int main(int argc, char **argv) {
    int control = argc == 2 && strcmp(argv[1], "--control") == 0;
    mach_port_t port = MACH_PORT_NULL;
    kern_return_t lookup = bootstrap_look_up(bootstrap_port, "com.apple.SecurityServer", &port);
    if (port != MACH_PORT_NULL) mach_port_deallocate(mach_task_self(), port);
    if ((control && lookup != KERN_SUCCESS) || (!control && lookup == KERN_SUCCESS)) return 90;
    const char *raw = getenv("FUSION_FIXTURE_NETWORK");
    if (raw == NULL) return 91;
    char *end = NULL;
    long number = strtol(raw, &end, 10);
    if (*end != '\0' || number < 1 || number > 65535) return 92;
    struct sockaddr_in address = {0};
    address.sin_family = AF_INET;
    address.sin_port = htons((unsigned short)number);
    address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    int fd = socket(AF_INET, SOCK_STREAM, 0);
    int connected = fd >= 0 && connect(fd, (struct sockaddr *)&address, sizeof(address)) == 0;
    if (fd >= 0) close(fd);
    if ((control && !connected) || (!control && connected)) return 93;
    pid_t child = fork();
    if (child == 0) _exit(0);
    if (child > 0) {
        int status = 0;
        waitpid(child, &status, 0);
        if (!control) return 94;
    } else if (control || errno != EPERM) return 95;
    puts(control ? "fixture-control-ready" : "fixture-completed");
    return 0;
}
