// bindings.dialog's C half: SDL's open and save dialogs for a program whose loop sleeps in SDL's
// event wait (`ui`'s). The dialogs are asynchronous — they return at once and answer through a
// callback, possibly on another thread, with a file list freed when the callback returns — and
// Lyra cannot hand C a function, so the callback copies the answer onto a queue here, under the
// tag the program asked with, and pushes an SDL event so the wait ends; the program reads the
// queue after its events (`lyra_dialog_poll`). `bindings/imgui/host.cpp` does the same for ImGui's
// host, which draws every frame and so needs no wake.
//
// SDL's functions are declared here rather than taken from its headers, as bindings.wake's are:
// the program that links this links SDL3, and the archive builds with nothing but a C compiler.

#include <pthread.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
    const char *name;
    const char *pattern;
} DialogFilter;
typedef void (*DialogCallback)(void *userdata, const char *const *filelist, int filter);

extern void SDL_ShowOpenFileDialog(DialogCallback callback, void *userdata, void *window,
                                   const DialogFilter *filters, int nfilters,
                                   const char *default_location, bool allow_many);
extern void SDL_ShowSaveFileDialog(DialogCallback callback, void *userdata, void *window,
                                   const DialogFilter *filters, int nfilters,
                                   const char *default_location);
extern const char *SDL_GetError(void);
extern uint32_t SDL_RegisterEvents(int numevents);
extern bool SDL_PushEvent(void *event);

enum { CHOSEN = 0, CANCELLED = 1, FAILED = 2 };

// An answer waiting to be read: its request's tag, what kind it is, the path or SDL's error.
typedef struct Answer {
    int32_t tag;
    int32_t kind;
    char *text;
    struct Answer *next;
} Answer;

// One dialog in flight: its tag, and the filters SDL reads until its callback.
typedef struct {
    int32_t tag;
    DialogFilter *filters;
    int nfilters;
    char *filter_text;
} Request;

static pthread_mutex_t lock = PTHREAD_MUTEX_INITIALIZER;
static Answer *answers = NULL;
static Answer *polled = NULL;
static uint32_t event_type = 0;

static char *copy_string(const char *s) {
    size_t n = strlen(s) + 1;
    char *out = (char *)malloc(n);
    if (out != NULL) memcpy(out, s, n);
    return out;
}

// The answer queued, and the program's wait woken.
static void queue(int32_t tag, int32_t kind, const char *text) {
    Answer *answer = (Answer *)calloc(1, sizeof(Answer));
    if (answer == NULL) return;
    answer->tag = tag;
    answer->kind = kind;
    answer->text = text != NULL ? copy_string(text) : NULL;
    pthread_mutex_lock(&lock);
    Answer **tail = &answers;
    while (*tail != NULL) tail = &(*tail)->next;
    *tail = answer;
    pthread_mutex_unlock(&lock);
    if (event_type != 0) {
        // SDL_Event is a 128-byte union whose first word is its type; the rest is zero.
        uint32_t event[32];
        memset(event, 0, sizeof event);
        event[0] = event_type;
        SDL_PushEvent(event);
    }
}

static void answered(void *userdata, const char *const *filelist, int filter) {
    (void)filter;
    Request *request = (Request *)userdata;
    if (filelist == NULL) {
        queue(request->tag, FAILED, SDL_GetError());
    } else if (filelist[0] == NULL) {
        queue(request->tag, CANCELLED, NULL);
    } else {
        queue(request->tag, CHOSEN, filelist[0]);
    }
    free(request->filters);
    free(request->filter_text);
    free(request);
}

// `filters` — "name\tpattern" lines, a pattern extensions joined by `;` or `*` — into SDL's
// array, owned by the request.
static bool parse_filters(Request *request, const char *filters) {
    request->filter_text = copy_string(filters);
    if (request->filter_text == NULL) return false;
    int lines = 0;
    for (const char *c = filters; *c != '\0'; c++)
        if (*c == '\n') lines++;
    if (filters[0] != '\0' && filters[strlen(filters) - 1] != '\n') lines++;
    if (lines == 0) return true;
    request->filters = (DialogFilter *)calloc((size_t)lines, sizeof(DialogFilter));
    if (request->filters == NULL) return false;
    char *line = request->filter_text;
    while (line != NULL && *line != '\0') {
        char *end = strchr(line, '\n');
        if (end != NULL) *end = '\0';
        char *tab = strchr(line, '\t');
        if (tab != NULL) {
            *tab = '\0';
            request->filters[request->nfilters].name = line;
            request->filters[request->nfilters].pattern = tab + 1;
            request->nfilters++;
        }
        line = end != NULL ? end + 1 : NULL;
    }
    return true;
}

// Show an open (`save` false) or save dialog, modal to `window`; its answer is queued under
// `tag`. `location` is where it starts, `""` for the system's choice. False only when out of
// memory, and then no answer will come.
bool lyra_dialog_show(void *window, int32_t tag, bool save, const char *filters,
                      const char *location) {
    if (event_type == 0) event_type = SDL_RegisterEvents(1);
    Request *request = (Request *)calloc(1, sizeof(Request));
    if (request == NULL) return false;
    request->tag = tag;
    if (!parse_filters(request, filters)) {
        free(request->filters);
        free(request->filter_text);
        free(request);
        return false;
    }
    DialogFilter *list = request->nfilters > 0 ? request->filters : NULL;
    const char *start = location[0] != '\0' ? location : NULL;
    if (save)
        SDL_ShowSaveFileDialog(answered, request, window, list, request->nfilters, start);
    else
        SDL_ShowOpenFileDialog(answered, request, window, list, request->nfilters, start, false);
    return true;
}

// The oldest answer waiting: its tag and kind through the pointers (`*kind` -1 for none), its
// path or error as the result (NULL for a cancel), owned here until the next poll.
const char *lyra_dialog_poll(int32_t *tag, int32_t *kind) {
    if (polled != NULL) {
        free(polled->text);
        free(polled);
        polled = NULL;
    }
    pthread_mutex_lock(&lock);
    Answer *answer = answers;
    if (answer != NULL) answers = answer->next;
    pthread_mutex_unlock(&lock);
    if (answer == NULL) {
        *kind = -1;
        return NULL;
    }
    polled = answer;
    *tag = answer->tag;
    *kind = answer->kind;
    return answer->text;
}

// Queue an answer as a dialog's callback would — for a test, with no dialog shown.
void lyra_dialog_answer(int32_t tag, int32_t kind, const char *text) {
    queue(tag, kind, text != NULL && text[0] != '\0' ? text : NULL);
}
