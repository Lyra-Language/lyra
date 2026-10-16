// The C half of `bindings.pty`: a program started on a pseudo-terminal, which a terminal
// emulator reads and writes from the other end.
//
// **Why C at all.** Each of these is a constant or a step that differs by platform, and Lyra
// cannot ask which platform it is on: `TIOCSWINSZ` and `TIOCSCTTY` (0x80087467 / 0x20007461
// on macOS, 0x5414 / 0x540E on Linux), `O_NONBLOCK` (4 vs 2048), the `WIFEXITED` encoding,
// and — most of all — the child's half of `fork`, which may call nothing that allocates or
// takes a lock (another thread may have held it when the process was copied). So the child
// runs only system calls here, on an argument and environment array built before the fork.
//
// `posix_openpt` rather than `forkpty`: `forkpty` lives in libutil on Linux (glibc before
// 2.34) and `<util.h>` on macOS, and needs a second library on the link line; the five calls
// below are in every libc.

#define _XOPEN_SOURCE 600
#define _DEFAULT_SOURCE
#define _DARWIN_C_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <termios.h>
#include <unistd.h>

extern char** environ;

// Whether `entry` ("NAME=value") sets one of the variables the child is given its own value
// of, so the parent's is left out of the copy.
static int overridden(const char* entry, const char* const* names) {
    for (; *names != NULL; names++) {
        size_t n = strlen(*names);
        if (strncmp(entry, *names, n) == 0 && entry[n] == '=')
            return 1;
    }
    return 0;
}

// Start `argv[0]` (a path, or a name found on the `PATH`) on a new pseudo-terminal `cols` ×
// `rows` cells, in `cwd` when not NULL, with this process's environment plus `extra_env`
// ("NAME=value" entries, NULL-terminated, each replacing any variable of that name). Answers
// the terminal's controlling end — non-blocking, close-on-exec — and sets `*pid`; -1 when it
// could not be opened or the program not started.
int32_t lyra_pty_spawn(const char* const* argv, const char* const* extra_env, const char* cwd,
                       int32_t cols, int32_t rows, int32_t* pid) {
    int master = posix_openpt(O_RDWR | O_NOCTTY);
    if (master < 0)
        return -1;
    if (grantpt(master) != 0 || unlockpt(master) != 0) {
        close(master);
        return -1;
    }
    const char* name = ptsname(master);
    if (name == NULL) {
        close(master);
        return -1;
    }
    // The slave is opened here, in the parent, so that a failure is an answer rather than
    // a child that exits at once; the child opens it again after `setsid` to make it its
    // controlling terminal.
    char slave_name[128];
    strncpy(slave_name, name, sizeof slave_name - 1);
    slave_name[sizeof slave_name - 1] = '\0';

    // The child's environment, built before the fork (allocation is not allowed after it).
    size_t n_env = 0, n_extra = 0;
    while (environ[n_env] != NULL)
        n_env++;
    while (extra_env[n_extra] != NULL)
        n_extra++;
    const char* names[64];
    char name_bufs[64][64];
    size_t n_names = 0;
    for (size_t i = 0; i < n_extra && n_names < 63; i++) {
        const char* eq = strchr(extra_env[i], '=');
        size_t len = eq ? (size_t)(eq - extra_env[i]) : strlen(extra_env[i]);
        if (len >= sizeof name_bufs[0])
            continue;
        memcpy(name_bufs[n_names], extra_env[i], len);
        name_bufs[n_names][len] = '\0';
        names[n_names] = name_bufs[n_names];
        n_names++;
    }
    names[n_names] = NULL;
    char** envp = malloc((n_env + n_extra + 1) * sizeof(char*));
    if (envp == NULL) {
        close(master);
        return -1;
    }
    size_t k = 0;
    for (size_t i = 0; i < n_env; i++)
        if (!overridden(environ[i], names))
            envp[k++] = environ[i];
    for (size_t i = 0; i < n_extra; i++)
        envp[k++] = (char*)extra_env[i];
    envp[k] = NULL;

    struct winsize size = {0};
    size.ws_col = (unsigned short)cols;
    size.ws_row = (unsigned short)rows;

    // A process that inherited SIGCHLD ignored has its children reaped by the kernel, and
    // `waitpid` can then never report how one ended. A terminal must know — it shows the
    // exit status — so the default is put back, once, here.
    struct sigaction chld;
    if (sigaction(SIGCHLD, NULL, &chld) == 0 && chld.sa_handler == SIG_IGN) {
        chld.sa_handler = SIG_DFL;
        sigaction(SIGCHLD, &chld, NULL);
    }

    pid_t child = fork();
    if (child < 0) {
        free(envp);
        close(master);
        return -1;
    }
    if (child == 0) {
        // The child: system calls only, until exec.
        setsid();
        int slave = open(slave_name, O_RDWR);
        if (slave < 0)
            _exit(127);
        ioctl(slave, TIOCSCTTY, 0);
        ioctl(slave, TIOCSWINSZ, &size);
        dup2(slave, 0);
        dup2(slave, 1);
        dup2(slave, 2);
        if (slave > 2)
            close(slave);
        close(master);
        // The parent may have ignored or blocked these; a shell expects the defaults.
        signal(SIGINT, SIG_DFL);
        signal(SIGQUIT, SIG_DFL);
        signal(SIGTSTP, SIG_DFL);
        signal(SIGTTIN, SIG_DFL);
        signal(SIGTTOU, SIG_DFL);
        signal(SIGCHLD, SIG_DFL);
        signal(SIGPIPE, SIG_DFL);
        sigset_t none;
        sigemptyset(&none);
        sigprocmask(SIG_SETMASK, &none, NULL);
        if (cwd != NULL)
            (void)chdir(cwd);
        environ = envp;
        execvp(argv[0], (char* const*)argv);
        _exit(127);
    }
    free(envp);
    ioctl(master, TIOCSWINSZ, &size);
    fcntl(master, F_SETFL, fcntl(master, F_GETFL) | O_NONBLOCK);
    fcntl(master, F_SETFD, FD_CLOEXEC);
    *pid = (int32_t)child;
    return master;
}

// Tell the program its terminal is now `cols` × `rows` cells (the kernel sends its
// foreground process group SIGWINCH). 0 when the terminal took it.
int32_t lyra_pty_resize(int32_t master, int32_t cols, int32_t rows) {
    struct winsize size = {0};
    size.ws_col = (unsigned short)cols;
    size.ws_row = (unsigned short)rows;
    return ioctl(master, TIOCSWINSZ, &size) == 0 ? 0 : -1;
}

// Up to `cap` bytes the program has written: their count, 0 when there is nothing yet, -1
// once the terminal is closed — every program holding it has ended (Linux answers `EIO`
// there, macOS end of file; both are the same answer).
int64_t lyra_pty_read(int32_t master, uint8_t* buf, int64_t cap) {
    for (;;) {
        ssize_t n = read(master, buf, (size_t)cap);
        if (n > 0)
            return (int64_t)n;
        if (n == 0)
            return -1;
        if (errno == EINTR)
            continue;
        if (errno == EAGAIN || errno == EWOULDBLOCK)
            return 0;
        return -1;
    }
}

// Write all `len` bytes to the program's input, waiting while the terminal's buffer is full
// (a large paste). 0 once all are taken, -1 when the terminal is closed.
int32_t lyra_pty_write(int32_t master, const uint8_t* bytes, int64_t len) {
    int64_t done = 0;
    while (done < len) {
        ssize_t n = write(master, bytes + done, (size_t)(len - done));
        if (n > 0) {
            done += n;
            continue;
        }
        if (n < 0 && errno == EINTR)
            continue;
        if (n < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
            usleep(1000);
            continue;
        }
        return -1;
    }
    return 0;
}

// The program's exit status once it has ended, without waiting: 1 with `*code` its exit code
// (128 + the signal's number when a signal ended it, as shells report it), 0 while it runs.
int32_t lyra_pty_exited(int32_t pid, int32_t* code) {
    int status = 0;
    pid_t r = waitpid((pid_t)pid, &status, WNOHANG);
    if (r == 0)
        return 0;
    if (r < 0) {
        *code = -1; // already reaped, or not ours
        return 1;
    }
    if (WIFEXITED(status))
        *code = WEXITSTATUS(status);
    else if (WIFSIGNALED(status))
        *code = 128 + WTERMSIG(status);
    else
        return 0; // stopped or continued: still there
    return 1;
}

// Hang up: close the controlling end (the session gets SIGHUP, as when a terminal window
// closes), and reap the program if it has already gone.
void lyra_pty_close(int32_t master, int32_t pid) {
    close(master);
    kill((pid_t)pid, SIGHUP);
    int status;
    (void)waitpid((pid_t)pid, &status, WNOHANG);
}
