// bindings.wake's C half: a thread that waits for file descriptors to have data and wakes
// SDL's event wait when one does, by pushing an event of its own. An interface's loop sleeps
// in SDL_WaitEventTimeout, which knows nothing of descriptors, so a terminal whose programs
// write would otherwise have to wake every few milliseconds to look.
//
// **One wake, then disarmed**: having pushed its event the thread waits until the main loop
// arms it again (lyra_wake_watch, called each pass after the loop has read what was ready) —
// otherwise a descriptor left readable would push events as fast as the thread could poll.
//
// SDL's two functions are declared here rather than taken from its headers: the program that
// links this links SDL3 (ui.sdl3 does), and the archive builds with nothing but a C compiler.

#include <poll.h>
#include <pthread.h>
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
        // SDL_Event is a 128-byte union whose first word is its type; the rest is zero.
        uint32_t event[32];
        memset(event, 0, sizeof event);
        event[0] = event_type;
        SDL_PushEvent(event);
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
        event_type = SDL_RegisterEvents(1);
        pthread_t thread;
        if (event_type == 0 || pipe(changes) != 0 ||
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
