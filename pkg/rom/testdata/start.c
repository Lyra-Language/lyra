/* The linker tests' runtime: a vector table and an entry, like runtime/genesis's. */
extern int main(void);
void _start(void);
__attribute__((section(".vectors"), used))
void (*const vectors[64])(void) = { (void (*)(void))0x01000000, _start };
void _start(void) { main(); for (;;) {} }
