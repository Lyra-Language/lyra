// The start of an iOS app built by lyrac: a program importing SDL3 for `--target ios` or
// `ios-simulator` (cmd/lyrac/app.go). Compiled for the platform and linked beside the
// program, whose own entry the backend emits as `lyra_main` (llvm.Backend.EntrySymbol).
//
// **Why not the program's `main`**: on iOS, SDL starts only inside UIKit's application —
// `SDL_Init` refuses otherwise ("did you include SDL_main.h?"), since there is no window
// scene and no run loop to show anything in. `SDL_RunApp` starts UIKit
// (`UIApplicationMain`) and, once it has launched, calls the function it is given on the main
// thread, inside the run loop: so a game's ordinary loop — poll, update, present — runs as it
// does on the Mac. What SDL's `SDL_main.h` macro does in C, by hand, since Lyra has no
// preprocessor to rename its `main`.
//
// **Why `exit`**: UIKit never returns from `UIApplicationMain`, and SDL does not exit when the
// function it called returns (src/video/uikit/SDL_uikitappdelegate.m) — an app whose game
// ended would sit on a black screen. Exiting with the program's status ends it as it ends on
// the Mac, which is what `lyrac run` and Vega's checks wait for.
//
// **Why the last line**: `simctl launch` answers 0 whatever the app's status, so the status
// is said on the way out — `lyra-exit: N`, the app's last line — and `lyrac run` takes it
// out of the output and exits with it (app.go's appOutput).
//
// No SDL header is needed: `SDL_RunApp` is declared here as SDL_main.h declares it.

#include <stdio.h>
#include <stdlib.h>

extern int lyra_main(int argc, char *argv[]);
extern int SDL_RunApp(int argc, char *argv[], int (*main_function)(int, char *[]), void *reserved);

static int run(int argc, char *argv[]) {
    int status = lyra_main(argc, argv);
    printf("lyra-exit: %d\n", status);
    fflush(stdout);
    exit(status);
}

int main(int argc, char *argv[]) {
    return SDL_RunApp(argc, argv, run, NULL);
}
