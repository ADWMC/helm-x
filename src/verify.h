// verify.h — built-in self-test / verification
#pragma once
namespace helmx {

// verify [--e2e] — run all checks, print report, exit 0 if all pass.
//   --e2e additionally runs `codex exec "zxwn"` and checks the activation reply.
int verify_main(int argc, char** argv);

}  // namespace helmx
