#include <node.h>
#include <stdlib.h>

int main(int argc, char** argv) {
    // Package lifecycle scripts stay disabled unless the caller explicitly opts in.
    setenv("NPM_CONFIG_IGNORE_SCRIPTS", "true", 0);
    return node::Start(argc, argv);
}
