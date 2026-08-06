// verify.h — built-in self-test / verification
#pragma once
#include <string>
namespace helmx {

// verify [--e2e] — run all checks, print report, exit 0 if all pass.
//   --e2e additionally runs `codex exec "helmx"` and checks the activation reply.
int verify_main(int argc, char** argv);

// Run checks into a report string (UI + CLI share this).
// Returns exit code; report receives the formatted output.
int run_verify(bool e2e, std::string& report);

// Run activation check. out = full codex reply, activated = phrase found.
// Returns 0 if activated, 1 otherwise (spawn fail or no phrase).
int run_zxwn(std::string& out, bool& activated);

// zxwn — send activation phrase via codex exec, print the reply.
//   Equivalent to: codex exec "helmx"
int zxwn_cmd();

// Shared: run `codex exec --skip-git-repo-check helmx`, capture stdout.
bool codex_exec_capture(std::string& out, int timeout_sec);

}  // namespace helmx
