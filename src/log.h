// log.h — file logging
#pragma once
#include <string>

namespace helmx {

std::string log_path();
std::string cyber_log_path();

void log_info(const std::string& msg);
void log_error(const std::string& msg);

// Cyber event logging (writes to helmx-cyber.log)
// original: the user's original request
// rewritten: the rewritten request (if any)
// result: "pass" / "blocked" / "rewritten_pass" / "rewritten_fail"
// status_code: HTTP status from upstream
void log_cyber(const std::string& original, const std::string& rewritten,
               const std::string& result, int status_code);

}  // namespace helmx
