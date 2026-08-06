// verify.h — built-in self-test / verification
#pragma once
namespace helmx {

// verify [--e2e] — run all checks, print report, exit 0 if all pass.
//   --e2e additionally runs `codex exec "zxwn"` and checks the activation reply.
int verify_main(int argc, char** argv);

// zxwn — send activation phrase via codex exec, print the reply.
//   Equivalent to: codex exec "zxwn"
//   Exit 0 if activation reply was detected, 1 otherwise.
int zxwn_cmd();

}  // namespace helmx
