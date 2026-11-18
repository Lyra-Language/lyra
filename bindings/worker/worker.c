// bindings.worker's C half: files read and written on a thread of their own, for a program whose
// loop sleeps in SDL's event wait (`ui`'s), so a large file or a slow disk never holds a frame.
// **One thread takes the jobs in the order they were asked**, so a write and then a read of the
// same file read what was written, as they did when `ui.app` did them in place; each answer is
// queued here under the tag the program asked with, and an SDL event is pushed so the wait
// ends; the program reads the queue after its events (`lyra_worker_poll`), as `bindings.dialog`
// does its dialogs' answers.
//
// SDL's functions are declared here rather than taken from its headers, as bindings.dialog's
// are: the program that links this links SDL3, and the archive builds with nothing but a C
// compiler.

// read, write, creat and close are POSIX's: glibc declares them under -std=c11 only so.
#define _POSIX_C_SOURCE 200809L

#include <fcntl.h>
#include <pthread.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

extern uint32_t SDL_RegisterEvents(int numevents);
extern bool SDL_PushEvent(void *event);

enum { READ = 0, WRITE = 1 };

// A request, then its answer: its tag, what it is, the path, the text to write or the text
// read, its length, and whether it succeeded.
typedef struct Job {
    int32_t tag;
    int32_t kind;
    char *path;
    char *text;
    int64_t length;
    bool ok;
    struct Job *next;
} Job;

static pthread_mutex_t lock = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t asked = PTHREAD_COND_INITIALIZER;
static Job *jobs = NULL;
static Job *answers = NULL;
static Job *polled = NULL;
static uint32_t event_type = 0;
static bool running = false;

// `job` put at the end of the list `head` (under the lock).
static void append(Job **head, Job *job) {
    Job **tail = head;
    while (*tail != NULL) tail = &(*tail)->next;
    job->next = NULL;
    *tail = job;
}

static char *copy_bytes(const char *s, int64_t n) {
    char *out = (char *)malloc((size_t)n + 1);
    if (out == NULL) return NULL;
    memcpy(out, s, (size_t)n);
    out[n] = '\0';
    return out;
}

// The answer queued, and the program's wait woken.
static void answered(Job *job) {
    pthread_mutex_lock(&lock);
    append(&answers, job);
    pthread_mutex_unlock(&lock);
    if (event_type != 0) {
        // SDL_Event is a 128-byte union whose first word is its type; the rest is zero.
        uint32_t event[32];
        memset(event, 0, sizeof event);
        event[0] = event_type;
        SDL_PushEvent(event);
    }
}

// The whole file at the job's path, as std.io's read_file reads it.
static void read_whole(Job *job) {
    int fd = open(job->path, O_RDONLY);
    if (fd < 0) return;
    int64_t cap = 65536, n = 0;
    char *buf = (char *)malloc((size_t)cap);
    while (buf != NULL) {
        if (n == cap) {
            cap *= 2;
            char *grown = (char *)realloc(buf, (size_t)cap);
            if (grown == NULL) {
                free(buf);
                buf = NULL;
                break;
            }
            buf = grown;
        }
        ssize_t got = read(fd, buf + n, (size_t)(cap - n));
        if (got <= 0) break;
        n += got;
    }
    close(fd);
    if (buf == NULL) return;
    job->text = buf;
    job->length = n;
    job->ok = true;
}

// The job's text as the whole of the file at its path, created rw-r--r-- before the umask, as
// std.io's write_file does; ok only when all of it landed.
static void write_whole(Job *job) {
    int fd = creat(job->path, 0644);
    if (fd < 0) return;
    int64_t done = 0;
    while (done < job->length) {
        ssize_t put = write(fd, job->text + done, (size_t)(job->length - done));
        if (put <= 0) break;
        done += put;
    }
    job->ok = close(fd) == 0 && done == job->length;
    free(job->text);
    job->text = NULL;
    job->length = 0;
}

// The worker: each job in turn, for as long as the program runs.
static void *run(void *arg) {
    (void)arg;
    for (;;) {
        pthread_mutex_lock(&lock);
        while (jobs == NULL) pthread_cond_wait(&asked, &lock);
        Job *job = jobs;
        jobs = job->next;
        pthread_mutex_unlock(&lock);
        if (job->kind == READ) read_whole(job);
        else write_whole(job);
        answered(job);
    }
    return NULL;
}

// A job queued for the worker, which starts with the first; false when it could not be (no
// memory, no thread), and then no answer will come.
static bool start(int32_t tag, int32_t kind, const char *path, const char *text, int64_t length) {
    if (event_type == 0) event_type = SDL_RegisterEvents(1);
    Job *job = (Job *)calloc(1, sizeof(Job));
    if (job == NULL) return false;
    job->tag = tag;
    job->kind = kind;
    job->path = copy_bytes(path, (int64_t)strlen(path));
    if (kind == WRITE) {
        job->text = copy_bytes(text, length);
        job->length = length;
    }
    bool made = job->path != NULL && (kind == READ || job->text != NULL);
    pthread_mutex_lock(&lock);
    if (made && !running) {
        pthread_t thread;
        pthread_attr_t attr;
        running = pthread_attr_init(&attr) == 0 &&
                  pthread_attr_setdetachstate(&attr, PTHREAD_CREATE_DETACHED) == 0 &&
                  pthread_create(&thread, &attr, run, NULL) == 0;
    }
    bool queued = made && running;
    if (queued) {
        append(&jobs, job);
        pthread_cond_signal(&asked);
    }
    pthread_mutex_unlock(&lock);
    if (!queued) {
        free(job->path);
        free(job->text);
        free(job);
    }
    return queued;
}

// Read the file at `path` off the loop; its text comes from the queue under `tag`.
bool lyra_worker_read(int32_t tag, const char *path) { return start(tag, READ, path, NULL, 0); }

// Write `length` bytes of `text` as the file at `path` off the loop; whether it landed comes
// from the queue under `tag`.
bool lyra_worker_write(int32_t tag, const char *path, const char *text, int64_t length) {
    return start(tag, WRITE, path, text, length);
}

// The oldest answer waiting: its tag, kind (-1 for none), whether it succeeded and a read's
// length through the pointers, a read's bytes as the result (NULL otherwise), owned here until
// the next poll.
const char *lyra_worker_poll(int32_t *tag, int32_t *kind, bool *ok, int64_t *length) {
    if (polled != NULL) {
        free(polled->path);
        free(polled->text);
        free(polled);
        polled = NULL;
    }
    pthread_mutex_lock(&lock);
    Job *job = answers;
    if (job != NULL) answers = job->next;
    pthread_mutex_unlock(&lock);
    if (job == NULL) {
        *kind = -1;
        return NULL;
    }
    polled = job;
    *tag = job->tag;
    *kind = job->kind;
    *ok = job->ok;
    *length = job->length;
    return job->text;
}
