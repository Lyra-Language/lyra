// bindings.wake's C half: a thread that waits for file descriptors to have data and wakes
// SDL's event wait when one does, by pushing an event of its own. An interface's loop sleeps
// in SDL_WaitEventTimeout, which knows nothing of descriptors, so a terminal whose programs
// write would otherwise have to wake every few milliseconds to look.
//
// **One wake, then disarmed**: having pushed its event the thread waits until the main loop
// arms it again (lyra_wake_watch, called each pass after the loop has read what was ready) —
// otherwise a descriptor left readable would push events as fast as the thread could poll.
//
// **A quit signal wakes it too** (lyra_wake_on_quit_signals): SDL's own SIGINT and SIGTERM
// handler only marks a quit for the next time events are pumped, and a loop asleep in a native
// wait (Cocoa's) pumps nothing until input comes — `pkill` left an idle window open. A handler
// put in front of SDL's runs it, then writes a byte to a pipe, and a thread reading the pipe
// pushes the wake; the wait returns, and the next pump sends SDL's quit as it always would.
//
// SDL's two functions are declared here rather than taken from its headers: the program that
// links this links SDL3 (ui.sdl3 does), and the archive builds with nothing but a C compiler.

// sigaction and siginfo_t are POSIX's, which a strict -std=c11 hides on glibc.
#define _POSIX_C_SOURCE 200809L

#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <pthread.h>
#include <signal.h>
#include <stdbool.h>
#include <stdint.h>
#include <string.h>
#include <unistd.h>

extern uint32_t SDL_RegisterEvents(int numevents);
extern bool SDL_PushEvent(void *event);

#define MAX_FDS 256

static pthread_mutex_t lock = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t arm = PTHREAD_COND_INITIALIZER;
static int watched[MAX_FDS];
static int count = 0;
static bool armed = false;
static bool started = false;
static int changes[2] = {-1, -1};  // written when the set changes, so a poll in progress sees it
static uint32_t event_type = 0;

// The event type the wake is pushed as, registered the first time it is asked for. Called
// with the lock held.
static uint32_t wake_type(void) {
    if (event_type == 0)
        event_type = SDL_RegisterEvents(1);
    return event_type;
}

// The wake itself: SDL_Event is a 128-byte union whose first word is its type; the rest is zero.
static void push_wake(void) {
    uint32_t event[32];
    memset(event, 0, sizeof event);
    event[0] = event_type;
    SDL_PushEvent(event);
}

static void *watch(void *unused) {
    (void)unused;
    struct pollfd fds[MAX_FDS + 1];
    for (;;) {
        pthread_mutex_lock(&lock);
        while (!armed)
            pthread_cond_wait(&arm, &lock);
        int n = count;
        fds[0].fd = changes[0];
        fds[0].events = POLLIN;
        fds[0].revents = 0;
        for (int i = 0; i < n; i++) {
            fds[i + 1].fd = watched[i];
            fds[i + 1].events = POLLIN;
            fds[i + 1].revents = 0;
        }
        pthread_mutex_unlock(&lock);
        if (poll(fds, (nfds_t)(n + 1), -1) <= 0)
            continue;
        if (fds[0].revents) {
            char drain[64];
            (void)!read(changes[0], drain, sizeof drain);
        }
        bool ready = false;
        for (int i = 0; i < n; i++) {
            if (fds[i + 1].revents & (POLLIN | POLLHUP | POLLERR))
                ready = true;
        }
        if (!ready)
            continue;
        pthread_mutex_lock(&lock);
        armed = false;
        pthread_mutex_unlock(&lock);
        push_wake();
    }
    return NULL;
}

// Watch these descriptors (at most 256; none stops watching), armed to wake once when one
// has data, its other end closes or it fails. Call it each pass of the loop, after reading
// what was ready. Answers the event type the wake is pushed as, or 0 if the thread could not
// be started.
uint32_t lyra_wake_watch(const int32_t *fds, int32_t n) {
    if (n > MAX_FDS)
        n = MAX_FDS;
    if (n < 0)
        n = 0;
    pthread_mutex_lock(&lock);
    if (!started) {
        pthread_t thread;
        if (wake_type() == 0 || pipe(changes) != 0 ||
            pthread_create(&thread, NULL, watch, NULL) != 0) {
            pthread_mutex_unlock(&lock);
            return 0;
        }
        pthread_detach(thread);
        started = true;
    }
    bool changed = n != count;
    for (int i = 0; i < n && !changed; i++)
        changed = watched[i] != fds[i];
    for (int i = 0; i < n; i++)
        watched[i] = fds[i];
    count = n;
    armed = n > 0;
    pthread_cond_signal(&arm);
    pthread_mutex_unlock(&lock);
    if (changed)
        (void)!write(changes[1], "x", 1);
    return event_type;
}

// ── Quit signals ──────────────────────────────────────────────────────────────────────────

static const int quit_signals[2] = {SIGINT, SIGTERM};
static struct sigaction before[2];  // the handler each had — SDL's — run first
static bool installed[2] = {false, false};
static int signalled[2] = {-1, -1};  // written by the handler, read by pass_signals
static bool passing = false;         // the pipe and its thread made

// Whether a disposition is a function to call: neither the default nor ignored. The two
// handler fields share their storage, so either shape reads the same pointer.
static bool calls(const struct sigaction *a) {
    return a->sa_handler != SIG_DFL && a->sa_handler != SIG_IGN;
}

static void on_quit_signal(int sig, siginfo_t *info, void *context) {
    int saved = errno;
    for (int i = 0; i < 2; i++) {
        if (quit_signals[i] != sig || !calls(&before[i]))
            continue;
        if (before[i].sa_flags & SA_SIGINFO)
            before[i].sa_sigaction(sig, info, context);
        else
            before[i].sa_handler(sig);
    }
    (void)!write(signalled[1], "q", 1);
    errno = saved;
}

static void *pass_signals(void *unused) {
    (void)unused;
    char got[16];
    for (;;) {
        ssize_t n = read(signalled[0], got, sizeof got);
        if (n < 0 && errno == EINTR)
            continue;
        if (n <= 0)
            return NULL;
        push_wake();
    }
}

// The pipe the handler writes and the thread passing it on, made once. Called with the lock held.
static bool start_passing(void) {
    if (passing)
        return true;
    pthread_t thread;
    if (wake_type() == 0 || pipe(signalled) != 0)
        return false;
    // Neither end handed to a program the loop starts (a terminal's shell), and the handler
    // never blocks on a full pipe: one byte waiting is as good as many.
    for (int i = 0; i < 2; i++)
        (void)fcntl(signalled[i], F_SETFD, FD_CLOEXEC);
    (void)fcntl(signalled[1], F_SETFL, fcntl(signalled[1], F_GETFL) | O_NONBLOCK);
    if (pthread_create(&thread, NULL, pass_signals, NULL) != 0) {
        close(signalled[0]);
        close(signalled[1]);
        return false;
    }
    pthread_detach(thread);
    passing = true;
    return true;
}

// Have SIGINT and SIGTERM wake the loop's SDL wait as well as do what their handlers did: call
// it after SDL_Init, whose handlers it puts itself in front of, and lyra_wake_off_quit_signals
// before SDL_Quit, which takes its handlers away only if they are still in place. A signal left
// at its default (which ends the program anyway) or ignored is left as it was. The handler found
// is called as it was installed: SDL's may carry SA_SIGINFO, which macOS keeps across exec from
// a parent's handler (a Go parent's) while resetting the handler, and SDL sets only the handler.
// Answers whether the wake is in place for either.
bool lyra_wake_on_quit_signals(void) {
    pthread_mutex_lock(&lock);
    bool any = false;
    if (start_passing()) {
        for (int i = 0; i < 2; i++) {
            if (installed[i]) {
                any = true;
                continue;
            }
            if (sigaction(quit_signals[i], NULL, &before[i]) != 0)
                continue;
            if (!calls(&before[i]))
                continue;
            struct sigaction mine;
            memset(&mine, 0, sizeof mine);
            mine.sa_sigaction = on_quit_signal;
            sigemptyset(&mine.sa_mask);
            mine.sa_flags = SA_SIGINFO | SA_RESTART;
            if (sigaction(quit_signals[i], &mine, NULL) == 0)
                installed[i] = any = true;
        }
    }
    pthread_mutex_unlock(&lock);
    return any;
}

// The handlers lyra_wake_on_quit_signals found put back, where its own is still in place.
void lyra_wake_off_quit_signals(void) {
    pthread_mutex_lock(&lock);
    for (int i = 0; i < 2; i++) {
        if (!installed[i])
            continue;
        struct sigaction now;
        if (sigaction(quit_signals[i], NULL, &now) == 0 && now.sa_sigaction == on_quit_signal)
            (void)sigaction(quit_signals[i], &before[i], NULL);
        installed[i] = false;
    }
    pthread_mutex_unlock(&lock);
}
