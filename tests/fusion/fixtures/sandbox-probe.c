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
#include <stdint.h>
#include <dlfcn.h>

/* Use the system ICU ABI without bundling headers or an alternate data set. */
static int timezone_probe(void) {
    void *icu = dlopen("/usr/lib/libicucore.A.dylib", RTLD_NOW | RTLD_LOCAL);
    if (icu == NULL) return 96;
    void *(*open_zones)(int, const char *, const int32_t *, int32_t *) =
        dlsym(icu, "ucal_openTimeZoneIDEnumeration");
    int32_t (*count_zones)(void *, int32_t *) = dlsym(icu, "uenum_count");
    void (*close_zones)(void *) = dlsym(icu, "uenum_close");
    if (open_zones == NULL || count_zones == NULL || close_zones == NULL) return 97;
    int32_t status = 0;
    void *zones = open_zones(0, NULL, NULL, &status); /* UCAL_ZONE_TYPE_ANY */
    if (zones == NULL || status > 0) return 98;
    int32_t count = count_zones(zones, &status);
    close_zones(zones);
    dlclose(icu);
    if (status > 0 || count < 1) return 99;
    puts("fixture-completed");
    return 0;
}

static int connect_port(const char *raw) {
    char *end = NULL;
    long number = strtol(raw, &end, 10);
    if (*end != '\0' || number < 1 || number > 65535) return 0;
    struct sockaddr_in address = {0};
    address.sin_family = AF_INET;
    address.sin_port = htons((unsigned short)number);
    address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    int fd = socket(AF_INET, SOCK_STREAM, 0);
    int connected = fd >= 0 && connect(fd, (struct sockaddr *)&address, sizeof(address)) == 0;
    if (fd >= 0) close(fd);
    return connected;
}

static int connect_port6(const char *raw) {
    char *end = NULL;
    long number = strtol(raw, &end, 10);
    if (*end != '\0' || number < 1 || number > 65535) return 0;
    struct sockaddr_in6 address = {0};
    address.sin6_family = AF_INET6;
    address.sin6_port = htons((unsigned short)number);
    address.sin6_addr = in6addr_loopback;
    int fd = socket(AF_INET6, SOCK_STREAM, 0);
    int connected = fd >= 0 && connect(fd, (struct sockaddr *)&address, sizeof(address)) == 0;
    if (fd >= 0) close(fd);
    return connected;
}

static int channel_probe(int argc, char **argv) {
    int control = strcmp(argv[1], "--channel-control") == 0;
    int grok = strcmp(argv[1], "--grok-channel") == 0;
    if ((control && argc != 4) || (!control && argc != 5)) return 100;
    if (!connect_port(argv[2]) || !connect_port6(argv[2]) || connect_port(argv[3]) != control) return 101;
    if (!control) {
        if (grok) {
            const char *home = getenv("GROK_HOME");
            if (home == NULL || getenv("ANTHROPIC_API_KEY") != NULL || getenv("FUSION_MANAGEMENT_SECRET") != NULL) return 107;
            FILE *file = fopen("fixture.txt", "r");
            if (file == NULL) return 108;
            fclose(file);
            file = fopen("readonly-effect.txt", "w");
            if (file != NULL) { fclose(file); return 109; }
            file = fopen(argv[4], "r");
            if (file != NULL) { fclose(file); return 110; }
            file = fopen(argv[4], "w");
            if (file != NULL) { fclose(file); return 111; }
            char scratch[4096];
            if (snprintf(scratch, sizeof(scratch), "%s/probe-private.txt", home) >= (int)sizeof(scratch)) return 112;
            file = fopen(scratch, "w");
            if (file == NULL) return 113;
            fputs("synthetic private write", file);
            fclose(file);
        } else {
            const char *key = getenv("ANTHROPIC_API_KEY");
            const char *token = getenv("ANTHROPIC_AUTH_TOKEN");
            const char *model = getenv("ANTHROPIC_MODEL");
            const char *native_tmp = getenv("CLAUDE_CODE_TMPDIR");
            const char *tmp = getenv("TMPDIR");
            if (key == NULL || strncmp(key, "fgs_", 4) != 0 || token == NULL || strcmp(key, token) != 0 || model == NULL || strcmp(model, "glm-5.3") != 0 || native_tmp == NULL || tmp == NULL || strcmp(native_tmp, tmp) != 0 || getenv("FUSION_MANAGEMENT_SECRET") != NULL) return 102;
        }
        mach_port_t port = MACH_PORT_NULL;
        if (bootstrap_look_up(bootstrap_port, "com.apple.SecurityServer", &port) == KERN_SUCCESS) {
            mach_port_deallocate(mach_task_self(), port);
            return 103;
        }
        pid_t child = fork();
        if (child == 0) _exit(0);
        if (child > 0) { int status = 0; waitpid(child, &status, 0); return 104; }
        if (errno != EPERM) return 105;
        if (grok) { puts("fixture-completed"); return 0; }
        int ready = 0;
        for (int i = 0; i < 2000; i++) {
            if (access(argv[4], F_OK) == 0) { ready = 1; break; }
            usleep(1000);
        }
        if (!ready) return 106;
    }
    puts("fixture-completed");
    return 0;
}

int main(int argc, char **argv) {
    if (argc >= 2 && (strcmp(argv[1], "--channel") == 0 || strcmp(argv[1], "--channel-control") == 0 || strcmp(argv[1], "--grok-channel") == 0)) return channel_probe(argc, argv);
    if (argc == 2 && strcmp(argv[1], "--timezone") == 0) return timezone_probe();
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
