// log.h — file logging to ~/.codex/helmx.log (thread-safe)
#pragma once
#include <string>

namespace helmx {

// Append a timestamped line to the helmx log file.
// Log path: <codex home>/helmx.log (falls back to ./helmx.log).
void log_info(const std::string& msg);
void log_error(const std::string& msg);

// Resolve the log file path (for display).
std::string log_path();

}  // namespace helmx
