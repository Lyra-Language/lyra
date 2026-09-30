/* The linker tests' program: data, bss, constants, a call into another object, and a
 * helper that only the library defines. */
extern int helper(int);
short counter = 7;              /* .data: loaded from ROM, runs in RAM */
long zeroed[4];                 /* .bss */
const char message[] = "LYRA";  /* .rodata */
const char *where = message;    /* .data holding a pointer into ROM: an R_68K_32 */
int main(void) {
    zeroed[1] = counter;
    return helper(counter) + where[0];
}
