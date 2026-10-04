/* Synthetic macOS boundary probe. Never reads a Keychain item, writes a
 * preference, or queries account credentials. sandbox_check SPI filter values
 * are verified against Apple's WebKit Source/WTF/wtf/spi/darwin/SandboxSPI.h.
 * Private SPI is diagnostic only and must be reverified on another OS. */
#include <CoreFoundation/CoreFoundation.h>
#include <unistd.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <fcntl.h>
#include <sys/wait.h>
#include <sys/socket.h>
#include <arpa/inet.h>

extern int sandbox_check(pid_t, const char *, int, ...);
extern const int SANDBOX_CHECK_NO_REPORT;

static int connected(const char *port) {
    struct sockaddr_in address = {0};
    address.sin_family = AF_INET;
    address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    address.sin_port = htons((unsigned short)atoi(port));
    int fd = socket(AF_INET, SOCK_STREAM, 0);
    int ok = fd >= 0 && connect(fd, (struct sockaddr *)&address, sizeof(address)) == 0;
    if (fd >= 0) close(fd);
    return ok;
}
int main(int argc, char **argv) {
    if (argc != 4) return 60;
    int confined = strcmp(argv[1], "--confined") == 0;
    int pref = 6 | SANDBOX_CHECK_NO_REPORT;
    int codex = sandbox_check(getpid(), "user-preference-read", pref, "com.openai.codex");
    int other = sandbox_check(getpid(), "user-preference-read", pref, "com.fusion.gateway.unrelated");
    int pref_write = sandbox_check(getpid(), "user-preference-write", pref, "com.openai.codex");
    int keychain = sandbox_check(getpid(), "mach-lookup", 2 | SANDBOX_CHECK_NO_REPORT, "com.apple.securityd");
    int launchd = sandbox_check(getpid(), "mach-lookup", 2 | SANDBOX_CHECK_NO_REPORT, "com.apple.coreservices.launchservicesd");
    int shm = sandbox_check(getpid(), "ipc-posix-shm-write-data", SANDBOX_CHECK_NO_REPORT);
    if (codex || !CFPreferencesAppSynchronize(CFSTR("com.openai.codex"))) return 61;
    if (confined ? (!other || !pref_write || !keychain || !launchd || !shm) : (other || pref_write || keychain || launchd || shm)) return 62;
    if (connected(argv[3]) == confined) return 63;
    int fd = open(argv[2], O_RDONLY);
    if ((fd >= 0) == confined) { if (fd >= 0) close(fd); return 64; }
    if (fd >= 0) close(fd);
    fd = open(argv[2], O_WRONLY);
    if ((fd >= 0) == confined) { if (fd >= 0) close(fd); return 65; }
    if (fd >= 0) close(fd);
    pid_t child = fork();
    if (child == 0) _exit(0);
    if ((child >= 0) == confined) { if (child > 0) waitpid(child, NULL, 0); return 66; }
    if (child > 0 && waitpid(child, NULL, 0) != child) return 67;
    if (confined) {
        if (getenv("FUSION_MANAGEMENT_SECRET") || getenv("OPENAI_API_KEY") || getenv("ANTHROPIC_API_KEY")) return 68;
        const char *home = getenv("CODEX_HOME");
        const char *fixed = getenv("CFFIXED_USER_HOME");
        if (!home || !fixed || strcmp(fixed, getenv("HOME"))) return 69;
        char path[4096];
        if (snprintf(path, sizeof(path), "%s/private-probe.txt", home) >= (int)sizeof(path)) return 70;
        fd = open(path, O_WRONLY | O_CREAT | O_EXCL, 0600);
        if (fd < 0 || write(fd, "fixture", 7) != 7) return 71;
        close(fd);
        fd = open("readonly-effect.txt", O_WRONLY | O_CREAT, 0600);
        if (fd >= 0) { close(fd); return 72; }
    }
    puts("fixture-completed");
    return 0;
}
